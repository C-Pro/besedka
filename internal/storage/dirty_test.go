package storage

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"slices"
	"testing"

	"besedka/internal/auth"
	"besedka/internal/models"

	"go.etcd.io/bbolt"
)

func newTestStorage(t *testing.T) *BboltStorage {
	t.Helper()
	st, err := NewBboltStorage(filepath.Join(t.TempDir(), "test.db"), []byte("test-secret"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

type markerInfo struct {
	kind byte
	segs []string
	gen  uint64
}

func dumpMarkers(t *testing.T, s *BboltStorage) []markerInfo {
	t.Helper()
	var out []markerInfo
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketBackupDirty).ForEach(func(k, v []byte) error {
			kind, segs, err := decodeMarkerKey(k)
			if err != nil {
				return err
			}
			m := markerInfo{kind: kind, gen: binary.BigEndian.Uint64(v)}
			for _, s := range segs {
				m.segs = append(m.segs, string(s))
			}
			out = append(out, m)
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func hasMarker(ms []markerInfo, kind byte, segs ...string) bool {
	for _, m := range ms {
		if m.kind == kind && slices.Equal(m.segs, segs) {
			return true
		}
	}
	return false
}

func seqKey(seq int64) string {
	k := make([]byte, 8)
	binary.BigEndian.PutUint64(k, uint64(seq))
	return string(k)
}

// TestDirtyMarkersAllWritePaths verifies every mutating storage method leaves
// the dirty marker(s) an incremental backup needs to pick the change up.
func TestDirtyMarkersAllWritePaths(t *testing.T) {
	st := newTestStorage(t)

	if err := st.UpsertChat(models.Chat{ID: "c1", Name: "general"}); err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		name string
		op   func() error
		kind byte
		segs []string
	}{
		{"SetConfig", func() error { return st.SetConfig("greeting", "hi") }, markerKindKey, []string{"settings", "greeting"}},
		{"UpsertCredentials", func() error {
			return st.UpsertCredentials(auth.UserCredentials{User: models.User{ID: "u1", UserName: "alice"}})
		}, markerKindKey, []string{"users", "u1"}},
		{"UpsertUserSettings", func() error { return st.UpsertUserSettings("u1", models.UserSettings{}) }, markerKindKey, []string{"user_settings", "u1"}},
		{"UpsertChat", func() error { return st.UpsertChat(models.Chat{ID: "c1", Name: "general"}) }, markerKindKey, []string{"chats", "c1"}},
		{"UpsertMessage", func() error {
			return st.UpsertMessage(models.Message{ChatID: "c1", Seq: 1, UserID: "u1", Content: "hello"})
		}, markerKindKey, []string{"messages", "c1", seqKey(1)}},
		{"UpsertToken", func() error { return st.UpsertToken("u1", "tok-hash") }, markerKindKey, []string{"tokens_v2", "tok-hash"}},
		{"DeleteToken", func() error { return st.DeleteToken("tok-hash") }, markerKindKey, []string{"tokens_v2", "tok-hash"}},
		{"UpsertRegistrationToken", func() error { return st.UpsertRegistrationToken("u1", "reg") }, markerKindKey, []string{"registration_tokens", "u1"}},
		{"DeleteRegistrationToken", func() error { return st.DeleteRegistrationToken("u1") }, markerKindKey, []string{"registration_tokens", "u1"}},
		{"SaveVAPIDKeys", func() error { return st.SaveVAPIDKeys("priv", "pub") }, markerKindKey, []string{"vapid_keys", "vapid_keys"}},
		{"UpsertPushSubscription", func() error { return st.UpsertPushSubscription("u1", "https://ep", []byte("sub")) }, markerKindKey, []string{"push_subscriptions", "u1", "https://ep"}},
		{"DeletePushSubscription", func() error { return st.DeletePushSubscription("u1", "https://ep") }, markerKindKey, []string{"push_subscriptions", "u1", "https://ep"}},
		{"SaveLastSeenBatch", func() error {
			return st.SaveLastSeenBatch([]models.LastSeenEntry{{UserID: "u1", ChatID: "c1", Seq: 1}})
		}, markerKindKey, []string{"last_seen", "u1:c1"}},
		{"UpsertPasskey", func() error {
			return st.UpsertPasskey(auth.Passkey{ID: []byte("pk1"), UserID: "u1"})
		}, markerKindKey, []string{"passkey_credentials", "u1", "pk1"}},
		{"DeletePasskey", func() error { return st.DeletePasskey("u1", []byte("pk1")) }, markerKindKey, []string{"passkey_credentials", "u1", "pk1"}},
		{"UpsertFileMetadata", func() error { return st.UpsertFileMetadata(FileMetadata{ID: "f1", Hash: "h1"}) }, markerKindKey, []string{"files", "f1"}},
	}
	for _, step := range steps {
		if err := step.op(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if !hasMarker(dumpMarkers(t, st), step.kind, step.segs...) {
			t.Errorf("%s: missing dirty marker for %v", step.name, step.segs)
		}
	}

	// UpsertMessage must also dirty the chat record whose LastSeq it bumped.
	if !hasMarker(dumpMarkers(t, st), markerKindKey, "chats", "c1") {
		t.Error("UpsertMessage: missing dirty marker for chat LastSeq update")
	}

	// DeleteAllPasskeys drops the whole per-user bucket: bucket tombstone.
	if err := st.UpsertPasskey(auth.Passkey{ID: []byte("pk2"), UserID: "u1"}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteAllPasskeys("u1"); err != nil {
		t.Fatal(err)
	}
	if !hasMarker(dumpMarkers(t, st), markerKindBucket, "passkey_credentials", "u1") {
		t.Error("DeleteAllPasskeys: missing bucket tombstone marker")
	}

	// The internal chain-state write must NOT journal itself.
	if err := st.CommitBackup("backups/x", 1<<62); err != nil {
		t.Fatal(err)
	}
	for _, m := range dumpMarkers(t, st) {
		if m.kind == markerKindKey && slices.Equal(m.segs, []string{"settings", configKeyBackupState}) {
			t.Error("backup_state must not carry a dirty marker")
		}
	}
}

// TestCommitBackupGenerations proves the marker-clearing watermark: markers
// covered by the snapshot go away, markers written after it — including a
// re-put of a key the snapshot already shipped — survive for the next
// incremental.
func TestCommitBackupGenerations(t *testing.T) {
	st := newTestStorage(t)

	if err := st.SetConfig("a", "1"); err != nil {
		t.Fatal(err)
	}
	_, txid, _, err := st.IncrementalSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	// Writes racing the "upload": a new key and a re-put of a shipped key.
	if err := st.SetConfig("b", "1"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetConfig("a", "2"); err != nil {
		t.Fatal(err)
	}
	if err := st.CommitBackup("backups/k1", txid); err != nil {
		t.Fatal(err)
	}

	ms := dumpMarkers(t, st)
	if !hasMarker(ms, markerKindKey, "settings", "a") {
		t.Error("marker for key re-put during upload should survive clearing")
	}
	if !hasMarker(ms, markerKindKey, "settings", "b") {
		t.Error("marker for key written during upload should survive clearing")
	}

	lastKey, gotTxid, err := st.BackupState()
	if err != nil {
		t.Fatal(err)
	}
	if lastKey != "backups/k1" || gotTxid != txid {
		t.Errorf("BackupState = (%q, %d), want (backups/k1, %d)", lastKey, gotTxid, txid)
	}

	// A second commit at the current watermark clears the survivors.
	_, txid2, _, err := st.IncrementalSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CommitBackup("backups/k2", txid2); err != nil {
		t.Fatal(err)
	}
	if got := len(dumpMarkers(t, st)); got != 0 {
		t.Errorf("expected no markers after full commit, got %d", got)
	}
}

// openRawDB opens a plain bbolt database for use as an ApplyIncremental target.
func openRawDB(t *testing.T) *bbolt.DB {
	t.Helper()
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "target.db"), 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func rawGet(t *testing.T, db *bbolt.DB, path ...string) []byte {
	t.Helper()
	var out []byte
	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(path[0]))
		for _, seg := range path[1 : len(path)-1] {
			if b == nil {
				return nil
			}
			b = b.Bucket([]byte(seg))
		}
		if b == nil {
			return nil
		}
		out = bytes.Clone(b.Get([]byte(path[len(path)-1])))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestIncrementalSnapshotResolvesLiveState verifies capture semantics: the
// payload reflects the state at snapshot time, not the sequence of operations
// — put-then-delete ships a delete, delete-then-reput ships a put, and a
// deleted bucket ships a tombstone applied before recreated contents.
func TestIncrementalSnapshotResolvesLiveState(t *testing.T) {
	st := newTestStorage(t)

	// put-then-delete => delete wins.
	if err := st.UpsertToken("u1", "gone"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteToken("gone"); err != nil {
		t.Fatal(err)
	}
	// delete-then-reput => put wins.
	if err := st.UpsertToken("u1", "kept"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteToken("kept"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertToken("u2", "kept"); err != nil {
		t.Fatal(err)
	}
	// bucket tombstone plus recreation.
	if err := st.UpsertPasskey(auth.Passkey{ID: []byte("old"), UserID: "u1"}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteAllPasskeys("u1"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertPasskey(auth.Passkey{ID: []byte("new"), UserID: "u1"}); err != nil {
		t.Fatal(err)
	}

	payload, _, _, err := st.IncrementalSnapshot()
	if err != nil {
		t.Fatal(err)
	}

	// Apply onto a target pre-seeded with stale state the delta must erase.
	db := openRawDB(t)
	err = db.Update(func(tx *bbolt.Tx) error {
		tok, err := tx.CreateBucketIfNotExists([]byte("tokens_v2"))
		if err != nil {
			return err
		}
		if err := tok.Put([]byte("gone"), []byte("stale")); err != nil {
			return err
		}
		pk, err := tx.CreateBucketIfNotExists([]byte("passkey_credentials"))
		if err != nil {
			return err
		}
		u1, err := pk.CreateBucketIfNotExists([]byte("u1"))
		if err != nil {
			return err
		}
		return u1.Put([]byte("old"), []byte("stale"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyIncremental(db, payload); err != nil {
		t.Fatal(err)
	}

	if got := rawGet(t, db, "tokens_v2", "gone"); got != nil {
		t.Errorf("put-then-delete: key should be gone, got %q", got)
	}
	want := rawGet(t, db, "tokens_v2", "kept")
	if want == nil {
		t.Error("delete-then-reput: key should exist")
	}
	if got := rawGet(t, db, "passkey_credentials", "u1", "old"); got != nil {
		t.Error("bucket tombstone: stale passkey should be gone")
	}
	if got := rawGet(t, db, "passkey_credentials", "u1", "new"); got == nil {
		t.Error("recreated passkey should exist after tombstone")
	}
}

func TestIncrementalSnapshotEmpty(t *testing.T) {
	st := newTestStorage(t)
	payload, _, count, err := st.IncrementalSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("fresh database should report 0 changed entries, got %d", count)
	}
	if len(payload) != 4 || binary.BigEndian.Uint32(payload) != 0 {
		t.Errorf("fresh database should produce an empty payload, got %d bytes", len(payload))
	}
	db := openRawDB(t)
	if err := ApplyIncremental(db, payload); err != nil {
		t.Errorf("applying an empty payload should succeed: %v", err)
	}
}

func TestApplyIncrementalRejectsMalformed(t *testing.T) {
	st := newTestStorage(t)
	if err := st.SetConfig("a", "1"); err != nil {
		t.Fatal(err)
	}
	payload, _, _, err := st.IncrementalSnapshot()
	if err != nil {
		t.Fatal(err)
	}

	db := openRawDB(t)
	if err := ApplyIncremental(db, append(bytes.Clone(payload), 0xFF)); err == nil {
		t.Error("trailing bytes should be rejected")
	}
	if err := ApplyIncremental(db, payload[:2]); err == nil {
		t.Error("truncated payload should be rejected")
	}
	short := bytes.Clone(payload)
	binary.BigEndian.PutUint32(short, 2) // claims more entries than present
	if err := ApplyIncremental(db, short); err == nil {
		t.Error("entry-count mismatch should be rejected")
	}
}
