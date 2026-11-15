// Generic language-agnostic single-pass syntax lexer and DOM highlighter.
// Avoids regular expressions to keep scanning O(n) with bounded iterations.

export const COMMON_KEYWORDS = new Set([
    // Control flow
    'if', 'else', 'elif', 'elsif', 'switch', 'case', 'default', 'for', 'while',
    'do', 'loop', 'break', 'continue', 'return', 'yield', 'match', 'when',
    'goto', 'then', 'end', 'until', 'unless', 'done', 'fi', 'esac', 'in', 'of',
    'range', 'select', 'defer', 'fallthrough',
    // Declarations and bindings
    'function', 'func', 'fn', 'def', 'sub', 'proc', 'class', 'struct',
    'interface', 'trait', 'enum', 'union', 'type', 'typedef', 'namespace',
    'module', 'package', 'mod', 'crate', 'impl', 'extends', 'implements',
    'var', 'let', 'const', 'val', 'mut', 'local', 'global', 'static', 'final',
    'readonly', 'volatile', 'extern', 'inline', 'comptime',
    // Visibility, async, and imports
    'public', 'private', 'protected', 'internal', 'abstract', 'virtual',
    'override', 'sealed', 'open', 'async', 'await', 'suspend', 'unsafe', 'pub',
    'export', 'import', 'from', 'as', 'use', 'using', 'require', 'include',
    'echo', 'print',
    // Error handling
    'try', 'catch', 'finally', 'throw', 'throws', 'raise', 'except', 'panic',
    'recover',
    // OOP, logical words, literals, and primitive types
    'new', 'delete', 'this', 'self', 'super', 'typeof', 'instanceof', 'sizeof',
    'alignof', 'is', 'not', 'and', 'or', 'xor', 'with', 'pass', 'lambda', 'go',
    'chan', 'true', 'false', 'null', 'nil', 'none', 'None', 'True', 'False',
    'undefined', 'NaN', 'Infinity', 'void', 'any', 'bool', 'boolean', 'int',
    'int8', 'int16', 'int32', 'int64', 'uint', 'uint8', 'uint16', 'uint32',
    'uint64', 'usize', 'isize', 'float', 'float32', 'float64', 'double',
    'char', 'string', 'str', 'byte', 'rune', 'auto',
    // Common uppercase SQL keywords
    'SELECT', 'FROM', 'WHERE', 'INSERT', 'INTO', 'VALUES', 'UPDATE', 'SET',
    'DELETE', 'CREATE', 'ALTER', 'DROP', 'TABLE', 'INDEX', 'JOIN', 'INNER',
    'LEFT', 'RIGHT', 'OUTER', 'ON', 'GROUP', 'BY', 'ORDER', 'HAVING', 'LIMIT',
    'OFFSET', 'AS', 'AND', 'OR', 'NOT', 'NULL', 'TRUE', 'FALSE', 'UNION',
    'ALL', 'DISTINCT', 'EXISTS', 'IN', 'IS', 'LIKE', 'CASE', 'WHEN', 'THEN',
    'ELSE', 'END', 'PRIMARY', 'KEY', 'FOREIGN', 'REFERENCES', 'BEGIN',
    'COMMIT', 'ROLLBACK'
]);

const SINGLE_CHAR_OPS = new Set([
    '+', '-', '*', '/', '%', '=', '<', '>', '!', '&', '|', '^', '~', '?', ':'
]);

const TWO_CHAR_OPS = new Set([
    '==', '!=', '<=', '>=', ':=', '+=', '-=', '*=', '/=', '%=', '&=', '|=',
    '^=', '&&', '||', '??', '<<', '>>', '->', '=>', '::', '++', '--', '**'
]);

const THREE_CHAR_OPS = new Set([
    '===', '!==', '<<=', '>>=', '<=>', '...', '>>>'
]);

function isDigit(ch) {
    return ch >= '0' && ch <= '9';
}

function isHexDigit(ch) {
    return isDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F');
}

function isBinDigit(ch) {
    return ch === '0' || ch === '1';
}

function isOctDigit(ch) {
    return ch >= '0' && ch <= '7';
}

function isAlpha(ch) {
    return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z');
}

function isIdentStart(ch) {
    return isAlpha(ch) || ch === '_' || ch === '$';
}

function isIdentPart(ch) {
    return isIdentStart(ch) || isDigit(ch);
}

function isQuotePrefix(ch) {
    return ch === 'f' || ch === 'r' || ch === 'b' || ch === 'u' ||
        ch === 'F' || ch === 'R' || ch === 'B' || ch === 'U' || ch === 'L';
}

function peek(str, idx, offset = 0) {
    const pos = idx + offset;
    return pos >= 0 && pos < str.length ? str[pos] : '';
}

function peekBack(str, idx, offset = 1) {
    const pos = idx - offset;
    return pos >= 0 && pos < str.length ? str[pos] : '';
}

function peekBackNonSpace(str, idx) {
    let pos = idx - 1;
    let steps = 0;
    while (pos >= 0 && steps < 64) {
        const c = str[pos];
        if (c === '\n' || c === '\r') {
            return '';
        }
        if (c !== ' ' && c !== '\t') {
            return c;
        }
        pos--;
        steps++;
    }
    return '';
}

function hasClosingQuoteOnLine(str, start, quote) {
    let j = start;
    const len = str.length;
    while (j < len) {
        const c = str[j];
        if (c === '\n') {
            return false;
        }
        if (c === '\\') {
            j += j + 1 < len ? 2 : 1;
            continue;
        }
        if (c === quote) {
            return true;
        }
        j++;
    }
    return false;
}

function scanNumberEnd(input, i) {
    const len = input.length;
    const ch = input[i];
    const next = peek(input, i, 1);

    if (ch === '0' && (next === 'x' || next === 'X') && isHexDigit(peek(input, i, 2))) {
        let j = i + 2;
        while (j < len && (isHexDigit(input[j]) || input[j] === '_')) {
            j++;
        }
        while (j < len && isIdentPart(input[j])) {
            j++;
        }
        return j;
    }

    if (ch === '0' && (next === 'b' || next === 'B') && isBinDigit(peek(input, i, 2))) {
        let j = i + 2;
        while (j < len && (isBinDigit(input[j]) || input[j] === '_')) {
            j++;
        }
        while (j < len && isIdentPart(input[j])) {
            j++;
        }
        return j;
    }

    if (ch === '0' && (next === 'o' || next === 'O') && isOctDigit(peek(input, i, 2))) {
        let j = i + 2;
        while (j < len && (isOctDigit(input[j]) || input[j] === '_')) {
            j++;
        }
        while (j < len && isIdentPart(input[j])) {
            j++;
        }
        return j;
    }

    let j = i;
    if (ch === '.') {
        j++;
        while (j < len && (isDigit(input[j]) || input[j] === '_')) {
            j++;
        }
    } else {
        while (j < len) {
            const c = input[j];
            if (isDigit(c) || c === '_') {
                j++;
            } else if (c === "'" && isDigit(peek(input, j, 1))) {
                j++;
            } else {
                break;
            }
        }
        if (j < len && input[j] === '.' && isDigit(peek(input, j, 1))) {
            j++;
            while (j < len && (isDigit(input[j]) || input[j] === '_')) {
                j++;
            }
        }
    }

    if (j < len && (input[j] === 'e' || input[j] === 'E')) {
        const expNext = peek(input, j, 1);
        if (isDigit(expNext)) {
            j++;
            while (j < len && (isDigit(input[j]) || input[j] === '_')) {
                j++;
            }
        } else if ((expNext === '+' || expNext === '-') && isDigit(peek(input, j, 2))) {
            j += 2;
            while (j < len && (isDigit(input[j]) || input[j] === '_')) {
                j++;
            }
        }
    }

    while (j < len && isIdentPart(input[j])) {
        j++;
    }
    return j;
}

function matchOperator(input, i) {
    if (i + 3 <= input.length) {
        const three = input.slice(i, i + 3);
        if (THREE_CHAR_OPS.has(three)) {
            return three;
        }
    }
    if (i + 2 <= input.length) {
        const two = input.slice(i, i + 2);
        if (TWO_CHAR_OPS.has(two)) {
            return two;
        }
    }
    const one = input[i];
    if (SINGLE_CHAR_OPS.has(one)) {
        return one;
    }
    return '';
}

function coalesceTextTokens(rawTokens) {
    if (rawTokens.length === 0) {
        return rawTokens;
    }
    const merged = [];
    for (let idx = 0; idx < rawTokens.length; idx++) {
        const tok = rawTokens[idx];
        const prev = merged.length > 0 ? merged[merged.length - 1] : null;
        if (tok.type === 'text' && prev && prev.type === 'text') {
            prev.value += tok.value;
        } else {
            merged.push(tok);
        }
    }
    return merged;
}

export function tokenizeCode(input, options = {}) {
    if (typeof input !== 'string' || input.length === 0) {
        return [];
    }

    const len = input.length;
    const maxIterations = typeof options.maxIterations === 'number'
        ? options.maxIterations
        : len * 2 + 1;

    try {
        const rawTokens = [];
        const bracketStack = [];
        let pairCounter = 0;
        let iterations = 0;
        let i = 0;

        while (i < len) {
            iterations++;
            if (iterations > maxIterations) {
                return [{ type: 'text', value: input }];
            }

            const loopStart = i;
            const ch = input[i];
            const next = peek(input, i, 1);
            const prev = peekBack(input, i, 1);

            // 1. Multi-line comments: /* ... */ and <!-- ... -->
            if (ch === '/' && next === '*') {
                let j = i + 2;
                while (j < len) {
                    if (input[j] === '*' && peek(input, j, 1) === '/') {
                        j += 2;
                        break;
                    }
                    j++;
                }
                rawTokens.push({ type: 'comment', value: input.slice(i, j) });
                i = j;
                continue;
            }

            if (ch === '<' && next === '!' && peek(input, i, 2) === '-' && peek(input, i, 3) === '-') {
                let j = i + 4;
                while (j < len) {
                    if (input[j] === '-' && peek(input, j, 1) === '-' && peek(input, j, 2) === '>') {
                        j += 3;
                        break;
                    }
                    j++;
                }
                rawTokens.push({ type: 'comment', value: input.slice(i, j) });
                i = j;
                continue;
            }

            // 2. Single-line comments: //, --, and #
            if (ch === '/' && next === '/') {
                // Keep URL schemes such as https:// out of comment highlighting.
                if (!(prev === ':' && isAlpha(peekBack(input, i, 2)))) {
                    let j = i + 2;
                    while (j < len && input[j] !== '\n') {
                        j++;
                    }
                    rawTokens.push({ type: 'comment', value: input.slice(i, j) });
                    i = j;
                    continue;
                }
            }

            if (ch === '-' && next === '-') {
                const c2 = peek(input, i, 2);
                const isCommentPrefix = c2 === '' || c2 === ' ' || c2 === '\t' ||
                    c2 === '\n' || c2 === '\r' ||
                    (c2 === '-' && peek(input, i, 3) !== '>');
                if (!isIdentPart(prev) && prev !== ')' && prev !== ']' && isCommentPrefix) {
                    let j = i + 2;
                    while (j < len && input[j] !== '\n') {
                        j++;
                    }
                    rawTokens.push({ type: 'comment', value: input.slice(i, j) });
                    i = j;
                    continue;
                }
            }

            if (ch === '#') {
                if (!isIdentPart(prev)) {
                    const prevNs = peekBackNonSpace(input, i);
                    let hexLen = 0;
                    while (isHexDigit(peek(input, i, 1 + hexLen)) && hexLen <= 8) {
                        hexLen++;
                    }
                    const isHexColor = (prevNs === ':' || prevNs === '=' || prevNs === ',' || prevNs === '(') &&
                        (hexLen === 3 || hexLen === 4 || hexLen === 6 || hexLen === 8) &&
                        !isIdentPart(peek(input, i, 1 + hexLen));
                    if (isHexColor) {
                        const end = i + 1 + hexLen;
                        rawTokens.push({ type: 'text', value: input.slice(i, end) });
                        i = end;
                        continue;
                    }
                    let j = i + 1;
                    while (j < len && input[j] !== '\n') {
                        j++;
                    }
                    rawTokens.push({ type: 'comment', value: input.slice(i, j) });
                    i = j;
                    continue;
                }
            }

            // 3. Quoted strings: triple-quoted, double, single, and backtick
            if ((ch === '"' || ch === "'") && next === ch && peek(input, i, 2) === ch) {
                let j = i + 3;
                while (j < len) {
                    if (input[j] === '\\') {
                        j += j + 1 < len ? 2 : 1;
                        continue;
                    }
                    if (input[j] === ch && peek(input, j, 1) === ch && peek(input, j, 2) === ch) {
                        j += 3;
                        break;
                    }
                    j++;
                }
                rawTokens.push({ type: 'string', value: input.slice(i, j) });
                i = j;
                continue;
            }

            if (ch === '"' || ch === "'" || ch === '`') {
                if (ch === "'") {
                    if (isIdentPart(prev)) {
                        const allowPrefixed = isQuotePrefix(prev) &&
                            !isIdentPart(peekBack(input, i, 2)) &&
                            hasClosingQuoteOnLine(input, i + 1, "'");
                        if (!allowPrefixed) {
                            rawTokens.push({ type: 'text', value: ch });
                            i++;
                            continue;
                        }
                    }

                    if (isIdentStart(next)) {
                        let k = i + 1;
                        while (k < len && isIdentPart(input[k])) {
                            k++;
                        }
                        const prevNs = peekBackNonSpace(input, i);
                        const isLifetimeContext = prev === '&' || prev === '<' ||
                            prevNs === '<' || prevNs === ',' || prevNs === ':';
                        if (peek(input, k, 0) !== "'" && isLifetimeContext) {
                            rawTokens.push({ type: 'text', value: ch });
                            i++;
                            continue;
                        }
                    }
                }

                let j = i + 1;
                while (j < len) {
                    const c = input[j];
                    if (c === '\\') {
                        j += j + 1 < len ? 2 : 1;
                        continue;
                    }
                    if (c === ch) {
                        j++;
                        break;
                    }
                    if (c === '\n' && ch !== '`') {
                        break;
                    }
                    j++;
                }
                rawTokens.push({ type: 'string', value: input.slice(i, j) });
                i = j;
                continue;
            }

            // 4. Number literals
            if (!isIdentPart(prev) && (isDigit(ch) || (ch === '.' && isDigit(next)))) {
                const end = scanNumberEnd(input, i);
                rawTokens.push({ type: 'number', value: input.slice(i, end) });
                i = end;
                continue;
            }

            // 5. Identifiers and keywords
            if (isIdentStart(ch)) {
                let j = i + 1;
                while (j < len && isIdentPart(input[j])) {
                    j++;
                }
                const word = input.slice(i, j);
                const prevNs = peekBackNonSpace(input, i);
                if (COMMON_KEYWORDS.has(word) && (prevNs !== '.' || word === 'await')) {
                    rawTokens.push({ type: 'keyword', value: word });
                } else {
                    rawTokens.push({ type: 'text', value: word });
                }
                i = j;
                continue;
            }

            // 6. Matching brackets
            if (ch === '(' || ch === '[' || ch === '\x7b') {
                const tok = { type: 'text', value: ch };
                bracketStack.push({
                    open: ch,
                    close: ch === '(' ? ')' : ch === '[' ? ']' : '\x7d',
                    token: tok
                });
                rawTokens.push(tok);
                i++;
                continue;
            }

            if (ch === ')' || ch === ']' || ch === '\x7d') {
                let matchIdx = -1;
                for (let s = bracketStack.length - 1; s >= 0; s--) {
                    if (bracketStack[s].close === ch) {
                        matchIdx = s;
                        break;
                    }
                }
                if (matchIdx !== -1) {
                    const entry = bracketStack[matchIdx];
                    bracketStack.length = matchIdx;
                    const depth = matchIdx % 3;
                    const pairId = ++pairCounter;
                    entry.token.type = 'bracket';
                    entry.token.depth = depth;
                    entry.token.pairId = pairId;
                    rawTokens.push({ type: 'bracket', value: ch, depth, pairId });
                } else {
                    rawTokens.push({ type: 'text', value: ch });
                }
                i++;
                continue;
            }

            // 7. Common operators
            const op = matchOperator(input, i);
            if (op) {
                rawTokens.push({ type: 'operator', value: op });
                i += op.length;
                continue;
            }

            // 8. Fallback plain text
            rawTokens.push({ type: 'text', value: ch });
            i++;

            if (i <= loopStart) {
                i = loopStart + 1;
            }
        }

        return coalesceTextTokens(rawTokens);
    } catch (_) {
        return [{ type: 'text', value: input }];
    }
}

export function highlightCodeElement(codeEl, options = {}) {
    if (!codeEl || typeof codeEl.textContent !== 'string') {
        return false;
    }

    const raw = codeEl.textContent;
    if (!raw) {
        return false;
    }

    const tokens = tokenizeCode(raw, options);
    if (!tokens || tokens.length === 0 || (tokens.length === 1 && tokens[0].type === 'text')) {
        return false;
    }

    const doc = codeEl.ownerDocument || document;
    try {
        const frag = doc.createDocumentFragment();
        for (let idx = 0; idx < tokens.length; idx++) {
            const tok = tokens[idx];
            if (tok.type === 'text') {
                frag.appendChild(doc.createTextNode(tok.value));
            } else {
                const span = doc.createElement('span');
                if (tok.type === 'bracket') {
                    span.className = `hl-bracket hl-bracket-${tok.depth}`;
                    span.dataset.bracketPair = String(tok.pairId);
                } else {
                    span.className = `hl-${tok.type}`;
                }
                span.textContent = tok.value;
                frag.appendChild(span);
            }
        }

        codeEl.replaceChildren(frag);

        if (!codeEl.dataset.hlBound) {
            codeEl.dataset.hlBound = '1';
            const setPairActive = (pairId, active) => {
                if (!pairId) {
                    return;
                }
                const spans = codeEl.querySelectorAll('.hl-bracket');
                for (const sp of spans) {
                    if (sp.dataset.bracketPair === pairId) {
                        sp.classList.toggle('hl-bracket-active', active);
                    }
                }
            };

            codeEl.addEventListener('mouseover', (e) => {
                const target = e.target;
                if (target?.classList?.contains('hl-bracket')) {
                    setPairActive(target.dataset.bracketPair, true);
                }
            });

            codeEl.addEventListener('mouseout', (e) => {
                const target = e.target;
                if (target?.classList?.contains('hl-bracket')) {
                    const related = e.relatedTarget;
                    if (related?.dataset?.bracketPair === target.dataset.bracketPair) {
                        return;
                    }
                    setPairActive(target.dataset.bracketPair, false);
                }
            });
        }

        return true;
    } catch (_) {
        codeEl.textContent = raw;
        return false;
    }
}
