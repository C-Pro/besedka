// Client-side lightweight zero-dependency ID3 tag & file metadata extractor

export function extractID3FromBuffer(buffer, fileName) {
    const bytes = new Uint8Array(buffer);
    let title = '';
    let artist = '';

    // Check for ID3v2 header magic "ID3"
    if (bytes.length >= 10 && bytes[0] === 0x49 && bytes[1] === 0x44 && bytes[2] === 0x33) {
        const version = bytes[3];
        const flags = bytes[5];
        const tagSize = (bytes[6] << 21) | (bytes[7] << 14) | (bytes[8] << 7) | bytes[9];
        let offset = 10;
        if (flags & 0x40) { // extended header
            const extSize = (bytes[10] << 24) | (bytes[11] << 16) | (bytes[12] << 8) | bytes[13];
            offset += 4 + extSize;
        }

        const limit = Math.min(10 + tagSize, bytes.length);
        const utf8Decoder = new TextDecoder('utf-8');
        const utf16Decoder = new TextDecoder('utf-16');

        while (offset + 10 <= limit) {
            const frameID = String.fromCharCode(bytes[offset], bytes[offset + 1], bytes[offset + 2], bytes[offset + 3]);
            if (bytes[offset] === 0) break;

            let frameSize = 0;
            if (version === 4) {
                frameSize = (bytes[offset + 4] << 21) | (bytes[offset + 5] << 14) | (bytes[offset + 6] << 7) | bytes[offset + 7];
            } else {
                frameSize = (bytes[offset + 4] << 24) | (bytes[offset + 5] << 16) | (bytes[offset + 8] ? (bytes[offset + 6] << 8) | bytes[offset + 7] : bytes[offset + 7]);
            }

            offset += 10;
            if (frameSize <= 0 || offset + frameSize > limit) break;

            const frameBytes = bytes.subarray(offset, offset + frameSize);
            offset += frameSize;

            if (frameID === 'TIT2' || frameID === 'TT2') {
                title = decodeTextFrame(frameBytes, utf8Decoder, utf16Decoder);
            } else if (frameID === 'TPE1' || frameID === 'TP1') {
                artist = decodeTextFrame(frameBytes, utf8Decoder, utf16Decoder);
            }
        }
    }

    // Fallback to filename parsing if title is empty
    if (!title && fileName) {
        const baseName = fileName.replace(/\.[^/.]+$/, ""); // strip file extension
        const parts = baseName.split(' - ');
        if (parts.length >= 2) {
            if (!artist) artist = parts[0].trim();
            title = parts.slice(1).join(' - ').trim();
        } else {
            title = baseName.trim();
        }
    }

    return { title, artist };
}

function decodeTextFrame(frameBytes, utf8Decoder, utf16Decoder) {
    if (frameBytes.length < 2) return '';
    const encoding = frameBytes[0];
    const content = frameBytes.subarray(1);

    try {
        if (encoding === 0 || encoding === 3) {
            const str = utf8Decoder.decode(content);
            return str.replace(/\0/g, '').trim();
        } else if (encoding === 1 || encoding === 2) {
            const str = utf16Decoder.decode(content);
            return str.replace(/\0/g, '').trim();
        }
    } catch (e) {
        console.warn('ID3 text decode error:', e);
    }
    return '';
}
