// Client-side @user mention utilities.
//
// Mentions are handled entirely on the client: the "highlight if it mentions
// me" styling is viewer-specific, but the server broadcasts identical message
// HTML to every recipient, so the per-viewer decoration has to happen at render
// time anyway. The @token charset mirrors the server's username charset
// (see internal/content/content.go).

const MENTION_CHARS = 'a-zA-Z0-9._-';
// A leading boundary keeps us from matching mid-token, e.g. the "@" in an
// email address like foo@bar.com.
const mentionRegex = new RegExp(`(^|[^${MENTION_CHARS}])@([${MENTION_CHARS}]+)`, 'g');

function buildUserNameMap(users) {
    const map = new Map();
    for (const u of users || []) {
        if (u && u.userName) {
            map.set(u.userName.toLowerCase(), u);
        }
    }
    return map;
}

// Resolve a raw @token to a known userName (lowercased). Trailing ".", "-" and
// "_" are trimmed and retried so that "@alice." resolves to "alice".
function resolveUserName(token, nameMap) {
    let candidate = token.toLowerCase();
    while (candidate.length > 0) {
        if (nameMap.has(candidate)) {
            return candidate;
        }
        if (/[._-]$/.test(candidate)) {
            candidate = candidate.slice(0, -1);
            continue;
        }
        return null;
    }
    return null;
}

// getMentionedUserNames returns the set of known userNames (lowercased)
// mentioned in raw message text. Used to decide whether to play a sound.
export function getMentionedUserNames(rawText, users) {
    const result = new Set();
    if (!rawText) {
        return result;
    }
    const nameMap = buildUserNameMap(users);
    if (nameMap.size === 0) {
        return result;
    }
    mentionRegex.lastIndex = 0;
    let match;
    while ((match = mentionRegex.exec(rawText)) !== null) {
        const resolved = resolveUserName(match[2], nameMap);
        if (resolved) {
            result.add(resolved);
        }
    }
    return result;
}

function decorateTextNode(textNode, nameMap, selfName) {
    const text = textNode.nodeValue;
    mentionRegex.lastIndex = 0;
    let match;
    let lastIndex = 0;
    let frag = null;

    while ((match = mentionRegex.exec(text)) !== null) {
        const lead = match[1]; // boundary char, or '' at string start
        const token = match[2];
        const resolved = resolveUserName(token, nameMap);
        if (!resolved) {
            continue;
        }
        if (!frag) {
            frag = document.createDocumentFragment();
        }
        const atIndex = match.index + lead.length; // position of '@'
        if (atIndex > lastIndex) {
            frag.appendChild(document.createTextNode(text.slice(lastIndex, atIndex)));
        }
        // Preserve the as-typed casing for the portion that actually resolved.
        const shownName = token.slice(0, resolved.length);
        const span = document.createElement('span');
        span.className = resolved === selfName ? 'mention mention-self' : 'mention';
        span.textContent = `@${shownName}`;
        frag.appendChild(span);
        lastIndex = atIndex + 1 + shownName.length;
    }

    if (frag) {
        if (lastIndex < text.length) {
            frag.appendChild(document.createTextNode(text.slice(lastIndex)));
        }
        textNode.parentNode.replaceChild(frag, textNode);
    }
}

// decorateMentions walks the text nodes of a rendered message-content element
// and wraps recognised @mentions in <span class="mention"> (adding
// "mention-self" for the current viewer). It skips <a>, <code> and <pre> so
// links, autolinked emails and code stay untouched. Nodes are built with
// createElement/textContent only, so no HTML is ever parsed (XSS-safe).
export function decorateMentions(contentSpanEl, users, currentUserName) {
    if (!contentSpanEl) {
        return;
    }
    const nameMap = buildUserNameMap(users);
    if (nameMap.size === 0) {
        return;
    }
    const selfName = currentUserName ? currentUserName.toLowerCase() : null;

    const walker = document.createTreeWalker(contentSpanEl, NodeFilter.SHOW_TEXT, {
        acceptNode(node) {
            if (!node.nodeValue || node.nodeValue.indexOf('@') === -1) {
                return NodeFilter.FILTER_REJECT;
            }
            let el = node.parentNode;
            while (el && el !== contentSpanEl) {
                const tag = el.nodeName;
                if (tag === 'A' || tag === 'CODE' || tag === 'PRE') {
                    return NodeFilter.FILTER_REJECT;
                }
                el = el.parentNode;
            }
            return NodeFilter.FILTER_ACCEPT;
        },
    });

    // Collect first, then mutate: replacing nodes mid-walk is unsafe.
    const targets = [];
    let node;
    while ((node = walker.nextNode())) {
        targets.push(node);
    }
    for (const textNode of targets) {
        decorateTextNode(textNode, nameMap, selfName);
    }
}
