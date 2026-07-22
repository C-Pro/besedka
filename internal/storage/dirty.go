package storage

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"go.etcd.io/bbolt"
)

// Incremental backups are driven by a dirty-key journal: every mutation records
// a marker in the backup_dirty bucket within the same write transaction, so the
// journal can never disagree with the data. A marker stores only the location
// of the change and the transaction id that made it; the changed value itself
// is read live at capture time, which coalesces repeated writes to one key and
// keeps the journal bounded by the number of distinct keys ever touched.
//
// Marker key layout (unambiguous for arbitrary-byte segments):
//
//	kind    1 byte  ('k' = record key, 'b' = deleted bucket)
//	nSegs   1 byte
//	segs    nSegs × (uvarint length + bytes)
//
// For 'k' the segments are the bucket path followed by the record key; for 'b'
// they are the path of the deleted bucket. 'b' sorts before 'k', so a cursor
// scan yields bucket tombstones first — the order restore must apply them in.
//
// Marker value: uint64 big-endian transaction id. CommitBackup deletes markers
// with txid <= the snapshot's txid; markers written by transactions that raced
// the upload have a higher txid and survive for the next incremental.
const (
	markerKindKey    byte = 'k'
	markerKindBucket byte = 'b'
)

// Incremental payload entry kinds (see encodeEntry).
const (
	entryPut          byte = 0
	entryDelete       byte = 1
	entryDeleteBucket byte = 2
)

// configKeyBackupState is the settings key holding the incremental-backup
// chain state. It is written via setConfigRaw so it never dirties itself.
const configKeyBackupState = "backup_state"

// backupState records the last successfully uploaded artifact and the
// transaction id its snapshot covered.
type backupState struct {
	LastKey string `json:"lastKey"`
	TxID    uint64 `json:"txid"`
}

// markDirty journals a change at the given marker location in the same
// transaction that made it.
func markDirty(tx *bbolt.Tx, kind byte, segs [][]byte) error {
	d := tx.Bucket(bucketBackupDirty)
	if d == nil {
		return fmt.Errorf("backup_dirty bucket missing")
	}
	var val [8]byte
	binary.BigEndian.PutUint64(val[:], uint64(tx.ID()))
	return d.Put(encodeMarkerKey(kind, segs), val[:])
}

// dirtyPut writes key/value into b and journals the change. path is the full
// bucket path of b from the root.
func dirtyPut(tx *bbolt.Tx, b *bbolt.Bucket, path [][]byte, key, value []byte) error {
	if err := b.Put(key, value); err != nil {
		return err
	}
	return markDirty(tx, markerKindKey, appendSeg(path, key))
}

// dirtyDelete deletes key from b and journals the change. path is the full
// bucket path of b from the root.
func dirtyDelete(tx *bbolt.Tx, b *bbolt.Bucket, path [][]byte, key []byte) error {
	if err := b.Delete(key); err != nil {
		return err
	}
	return markDirty(tx, markerKindKey, appendSeg(path, key))
}

// dirtyDeleteBucket deletes the sub-bucket named by the last element of path
// from parent and journals a bucket tombstone. path is the full bucket path of
// the deleted bucket from the root.
func dirtyDeleteBucket(tx *bbolt.Tx, parent *bbolt.Bucket, path [][]byte) error {
	if err := parent.DeleteBucket(path[len(path)-1]); err != nil {
		return err
	}
	return markDirty(tx, markerKindBucket, path)
}

// appendSeg returns path + seg without aliasing path's backing array.
func appendSeg(path [][]byte, seg []byte) [][]byte {
	segs := make([][]byte, 0, len(path)+1)
	segs = append(segs, path...)
	return append(segs, seg)
}

func encodeMarkerKey(kind byte, segs [][]byte) []byte {
	n := 2
	for _, s := range segs {
		n += binary.MaxVarintLen64 + len(s)
	}
	out := make([]byte, 2, n)
	out[0] = kind
	out[1] = byte(len(segs))
	for _, s := range segs {
		out = binary.AppendUvarint(out, uint64(len(s)))
		out = append(out, s...)
	}
	return out
}

func decodeMarkerKey(data []byte) (kind byte, segs [][]byte, err error) {
	if len(data) < 2 {
		return 0, nil, fmt.Errorf("marker key too short")
	}
	kind = data[0]
	if kind != markerKindKey && kind != markerKindBucket {
		return 0, nil, fmt.Errorf("unknown marker kind %q", kind)
	}
	nSegs := int(data[1])
	rest := data[2:]
	segs = make([][]byte, 0, nSegs)
	for range nSegs {
		l, n := binary.Uvarint(rest)
		if n <= 0 || uint64(len(rest)-n) < l {
			return 0, nil, fmt.Errorf("truncated marker key segment")
		}
		segs = append(segs, rest[n:n+int(l)])
		rest = rest[n+int(l):]
	}
	if len(rest) != 0 {
		return 0, nil, fmt.Errorf("trailing bytes in marker key")
	}
	if kind == markerKindKey && nSegs < 2 {
		return 0, nil, fmt.Errorf("key marker needs a bucket path and a key")
	}
	if kind == markerKindBucket && nSegs < 1 {
		return 0, nil, fmt.Errorf("bucket marker needs a path")
	}
	return kind, segs, nil
}

// lookupBucket walks path from the transaction root, returning nil if any
// bucket along the way does not exist.
func lookupBucket(tx *bbolt.Tx, path [][]byte) *bbolt.Bucket {
	b := tx.Bucket(path[0])
	for _, seg := range path[1:] {
		if b == nil {
			return nil
		}
		b = b.Bucket(seg)
	}
	return b
}

// SnapshotToWithTxID is SnapshotTo but also reports the transaction id the
// snapshot covers, for use as the CommitBackup watermark.
func (s *BboltStorage) SnapshotToWithTxID(w io.Writer) (int64, uint64, error) {
	var (
		n    int64
		txid uint64
	)
	err := s.db.View(func(tx *bbolt.Tx) error {
		txid = uint64(tx.ID())
		var err error
		n, err = tx.WriteTo(w)
		return err
	})
	return n, txid, err
}

// IncrementalSnapshot serializes the current value (or deletion) of every
// dirty key into an incremental backup payload, captured in one read
// transaction. It returns the plaintext payload, the transaction id it covers,
// and the number of changed entries (0 when nothing changed since the last
// backup, letting the caller skip an empty upload); the caller encrypts the
// payload and, after a successful upload, calls CommitBackup with the txid.
//
// Payload layout: uint32 big-endian entry count, then entries:
//
//	kind    1 byte (0=put, 1=delete, 2=deleteBucket)
//	nSegs   1 byte, then nSegs × (uvarint length + bytes)  — bucket path
//	key     uvarint length + bytes                          — kinds 0 and 1
//	value   uvarint length + bytes                          — kind 0 only
//
// Bucket tombstones come first ('b' markers sort before 'k'), so applying
// entries in order deletes a bucket before re-putting its recreated contents.
func (s *BboltStorage) IncrementalSnapshot() (payload []byte, txid uint64, count int, err error) {
	var buf bytes.Buffer
	var n uint32
	err = s.db.View(func(tx *bbolt.Tx) error {
		txid = uint64(tx.ID())
		buf.Write([]byte{0, 0, 0, 0}) // entry count, fixed up below

		d := tx.Bucket(bucketBackupDirty)
		if d == nil {
			return fmt.Errorf("backup_dirty bucket missing")
		}
		c := d.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			kind, segs, err := decodeMarkerKey(k)
			if err != nil {
				return fmt.Errorf("corrupt dirty marker: %w", err)
			}
			switch kind {
			case markerKindBucket:
				encodeEntry(&buf, entryDeleteBucket, segs, nil, nil)
			case markerKindKey:
				path, key := segs[:len(segs)-1], segs[len(segs)-1]
				var value []byte
				if b := lookupBucket(tx, path); b != nil {
					value = b.Get(key)
				}
				if value == nil {
					encodeEntry(&buf, entryDelete, path, key, nil)
				} else {
					encodeEntry(&buf, entryPut, path, key, value)
				}
			}
			n++
		}
		return nil
	})
	if err != nil {
		return nil, 0, 0, err
	}
	payload = buf.Bytes()
	binary.BigEndian.PutUint32(payload[:4], n)
	return payload, txid, int(n), nil
}

func encodeEntry(buf *bytes.Buffer, kind byte, path [][]byte, key, value []byte) {
	buf.WriteByte(kind)
	buf.WriteByte(byte(len(path)))
	for _, seg := range path {
		writeUvarintBytes(buf, seg)
	}
	if kind != entryDeleteBucket {
		writeUvarintBytes(buf, key)
	}
	if kind == entryPut {
		writeUvarintBytes(buf, value)
	}
}

func writeUvarintBytes(buf *bytes.Buffer, b []byte) {
	var l [binary.MaxVarintLen64]byte
	buf.Write(l[:binary.PutUvarint(l[:], uint64(len(b)))])
	buf.Write(b)
}

// ApplyIncremental applies a decrypted incremental payload to db in a single
// write transaction. Puts recreate missing buckets along the path; deletes of
// absent keys or buckets are no-ops, making re-application of an already
// shipped change idempotent. A payload that does not decode exactly to its
// declared entry count and length is rejected.
func ApplyIncremental(db *bbolt.DB, payload []byte) error {
	if len(payload) < 4 {
		return fmt.Errorf("incremental payload too short")
	}
	count := binary.BigEndian.Uint32(payload[:4])
	r := bytes.NewReader(payload[4:])

	return db.Update(func(tx *bbolt.Tx) error {
		for i := uint32(0); i < count; i++ {
			kind, path, key, value, err := decodeEntry(r)
			if err != nil {
				return fmt.Errorf("incremental entry %d: %w", i, err)
			}
			switch kind {
			case entryDeleteBucket:
				parent, name := path[:len(path)-1], path[len(path)-1]
				if len(parent) == 0 {
					if tx.Bucket(name) != nil {
						if err := tx.DeleteBucket(name); err != nil {
							return err
						}
					}
				} else if b := lookupBucket(tx, parent); b != nil && b.Bucket(name) != nil {
					if err := b.DeleteBucket(name); err != nil {
						return err
					}
				}
			case entryPut:
				b, err := createBucketPath(tx, path)
				if err != nil {
					return err
				}
				if err := b.Put(key, value); err != nil {
					return err
				}
			case entryDelete:
				if b := lookupBucket(tx, path); b != nil {
					if err := b.Delete(key); err != nil {
						return err
					}
				}
			}
		}
		if r.Len() != 0 {
			return fmt.Errorf("incremental payload has %d trailing bytes", r.Len())
		}
		return nil
	})
}

func createBucketPath(tx *bbolt.Tx, path [][]byte) (*bbolt.Bucket, error) {
	b, err := tx.CreateBucketIfNotExists(path[0])
	if err != nil {
		return nil, err
	}
	for _, seg := range path[1:] {
		if b, err = b.CreateBucketIfNotExists(seg); err != nil {
			return nil, err
		}
	}
	return b, nil
}

func decodeEntry(r *bytes.Reader) (kind byte, path [][]byte, key, value []byte, err error) {
	kind, err = r.ReadByte()
	if err != nil {
		return 0, nil, nil, nil, err
	}
	if kind > entryDeleteBucket {
		return 0, nil, nil, nil, fmt.Errorf("unknown entry kind %d", kind)
	}
	nSegs, err := r.ReadByte()
	if err != nil {
		return 0, nil, nil, nil, err
	}
	if nSegs == 0 {
		return 0, nil, nil, nil, fmt.Errorf("empty bucket path")
	}
	path = make([][]byte, 0, nSegs)
	for range int(nSegs) {
		seg, err := readUvarintBytes(r)
		if err != nil {
			return 0, nil, nil, nil, err
		}
		path = append(path, seg)
	}
	if kind != entryDeleteBucket {
		if key, err = readUvarintBytes(r); err != nil {
			return 0, nil, nil, nil, err
		}
	}
	if kind == entryPut {
		if value, err = readUvarintBytes(r); err != nil {
			return 0, nil, nil, nil, err
		}
	}
	return kind, path, key, value, nil
}

func readUvarintBytes(r *bytes.Reader) ([]byte, error) {
	l, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, err
	}
	if uint64(r.Len()) < l {
		return nil, fmt.Errorf("truncated field: want %d bytes, have %d", l, r.Len())
	}
	b := make([]byte, l)
	_, _ = r.Read(b)
	return b, nil
}

// CommitBackup records a successful upload: it clears dirty markers covered by
// the snapshot (txid watermark) and persists the chain state, atomically.
// Markers written by transactions that raced the upload keep a higher txid and
// survive for the next incremental, so no change is ever silently dropped.
func (s *BboltStorage) CommitBackup(lastKey string, txid uint64) error {
	state, err := json.Marshal(backupState{LastKey: lastKey, TxID: txid})
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		d := tx.Bucket(bucketBackupDirty)
		if d == nil {
			return fmt.Errorf("backup_dirty bucket missing")
		}
		c := d.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if len(v) == 8 && binary.BigEndian.Uint64(v) > txid {
				continue
			}
			if err := c.Delete(); err != nil {
				return err
			}
		}
		return tx.Bucket(bucketSettings).Put([]byte(configKeyBackupState), state)
	})
}

// BackupState returns the last committed chain state; zero values mean no
// backup has been committed by this database yet.
func (s *BboltStorage) BackupState() (lastKey string, txid uint64, err error) {
	raw, err := s.GetConfig(configKeyBackupState)
	if err != nil || raw == "" {
		return "", 0, err
	}
	var st backupState
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return "", 0, fmt.Errorf("corrupt backup_state: %w", err)
	}
	return st.LastKey, st.TxID, nil
}
