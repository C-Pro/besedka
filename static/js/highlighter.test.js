import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { COMMON_KEYWORDS, highlightCodeElement, tokenizeCode } from './highlighter.js';

function joinTokens(tokens) {
    return tokens.map((t) => t.value).join('');
}

function tokensOfType(tokens, type) {
    return tokens.filter((t) => t.type === type);
}

function hasToken(tokens, type, value) {
    return tokens.some((t) => t.type === type && (value === undefined || t.value === value));
}

function createMockDOM() {
    class MockClassList {
        constructor(el) {
            this.el = el;
            this.set = new Set();
        }
        syncFromClassName(name) {
            this.set = new Set((name || '').split(' ').filter(Boolean));
        }
        add(...cls) {
            for (const c of cls) this.set.add(c);
            this.el._className = [...this.set].join(' ');
        }
        remove(...cls) {
            for (const c of cls) this.set.delete(c);
            this.el._className = [...this.set].join(' ');
        }
        toggle(cls, force) {
            if (force === undefined) {
                if (this.set.has(cls)) this.remove(cls);
                else this.add(cls);
                return this.set.has(cls);
            }
            if (force) this.add(cls);
            else this.remove(cls);
            return Boolean(force);
        }
        contains(cls) {
            return this.set.has(cls);
        }
    }

    class MockTextNode {
        constructor(text) {
            this.nodeValue = text;
        }
        get textContent() {
            return this.nodeValue;
        }
    }

    class MockFragment {
        constructor() {
            this.children = [];
        }
        appendChild(node) {
            this.children.push(node);
            return node;
        }
    }

    class MockElement {
        constructor(tagName, ownerDocument) {
            this.tagName = tagName;
            this.ownerDocument = ownerDocument;
            this.children = [];
            this.dataset = {};
            this.listeners = new Map();
            this._className = '';
            this.classList = new MockClassList(this);
            this._textContent = '';
        }
        set className(val) {
            this._className = val;
            this.classList.syncFromClassName(val);
        }
        get className() {
            return this._className;
        }
        set textContent(val) {
            this.children = [new MockTextNode(String(val))];
            this._textContent = String(val);
        }
        get textContent() {
            if (this.children.length === 0) return this._textContent;
            return this.children.map((c) => c.textContent).join('');
        }
        replaceChildren(node) {
            if (node instanceof MockFragment) {
                this.children = [...node.children];
            } else {
                this.children = [node];
            }
        }
        addEventListener(type, fn) {
            if (!this.listeners.has(type)) this.listeners.set(type, []);
            this.listeners.get(type).push(fn);
        }
        dispatchEvent(type, event) {
            for (const fn of this.listeners.get(type) || []) {
                fn(event);
            }
        }
        querySelectorAll(selector) {
            if (selector === '.hl-bracket') {
                return this.children.filter(
                    (c) => c instanceof MockElement && c.classList.contains('hl-bracket')
                );
            }
            return [];
        }
    }

    const doc = {
        createDocumentFragment: () => new MockFragment(),
        createTextNode: (text) => new MockTextNode(text),
        createElement: (tag) => new MockElement(tag, doc)
    };

    return { doc, MockElement, MockTextNode };
}

describe('tokenizeCode core token types', () => {
    test('highlights number literals across bases and formats', () => {
        const code = '0 42 3.14 .5 1e-9 2.5E+3 0xdeadBEEF 0b1010_0101 0o755 1_000_000 100n 3.14f 42u64 99L';
        const tokens = tokenizeCode(code);
        expect(joinTokens(tokens)).toBe(code);
        const nums = tokensOfType(tokens, 'number').map((t) => t.value);
        expect(nums).toEqual([
            '0', '42', '3.14', '.5', '1e-9', '2.5E+3',
            '0xdeadBEEF', '0b1010_0101', '0o755', '1_000_000',
            '100n', '3.14f', '42u64', '99L'
        ]);
    });

    test('does not highlight digits attached to identifiers as numbers', () => {
        const code = 'var v1 = item2 + _3;';
        const tokens = tokenizeCode(code);
        expect(joinTokens(tokens)).toBe(code);
        expect(tokensOfType(tokens, 'number')).toEqual([]);
    });

    test('highlights single-line and multi-line comments', () => {
        const code = [
            '// c-style comment',
            '# hash comment',
            '-- sql comment',
            '/* block\n   comment */',
            '<!-- html\n   comment -->'
        ].join('\n');
        const tokens = tokenizeCode(code);
        expect(joinTokens(tokens)).toBe(code);
        const comments = tokensOfType(tokens, 'comment').map((t) => t.value);
        expect(comments).toEqual([
            '// c-style comment',
            '# hash comment',
            '-- sql comment',
            '/* block\n   comment */',
            '<!-- html\n   comment -->'
        ]);
    });

    test('handles comment lookback/lookahead heuristics for CSS hex, URLs, and decrements', () => {
        const code = 'color: #ff00aa;\nurl = https://example.com;\ni--; --j; -->';
        const tokens = tokenizeCode(code);
        expect(joinTokens(tokens)).toBe(code);
        expect(tokensOfType(tokens, 'comment')).toEqual([]);
        expect(hasToken(tokens, 'operator', '--')).toBe(true);
    });

    test('handles unclosed multi-line comments safely to EOF', () => {
        const c1 = 'let a = 1; /* unterminated comment';
        const t1 = tokenizeCode(c1);
        expect(joinTokens(t1)).toBe(c1);
        expect(hasToken(t1, 'comment', '/* unterminated comment')).toBe(true);

        const c2 = '<div><!-- unterminated html comment';
        const t2 = tokenizeCode(c2);
        expect(joinTokens(t2)).toBe(c2);
        expect(hasToken(t2, 'comment', '<!-- unterminated html comment')).toBe(true);
    });

    test('highlights quoted strings and handles escaped quotes', () => {
        const code = `"hello \\"world\\" \\\\" 'a \\'b\\' \\\\' \`line1\nline2 \\\`ok\\\`\` """triple "quote" """ '''single 'triple' '''`;
        const tokens = tokenizeCode(code);
        expect(joinTokens(tokens)).toBe(code);
        const strs = tokensOfType(tokens, 'string').map((t) => t.value);
        expect(strs).toEqual([
            '"hello \\"world\\" \\\\"',
            "'a \\'b\\' \\\\'",
            '`line1\nline2 \\`ok\\``',
            '"""triple "quote" """',
            "'''single 'triple' '''"
        ]);
    });

    test('avoids false strings on contractions, C++ digit separators, and Rust lifetimes', () => {
        const code = "don't it's 1'000'000 fn foo<'a, 'static>(x: &'a str) -> char { return 'z'; }";
        const tokens = tokenizeCode(code);
        expect(joinTokens(tokens)).toBe(code);
        const strs = tokensOfType(tokens, 'string').map((t) => t.value);
        expect(strs).toEqual(["'z'"]);
        expect(hasToken(tokens, 'number', "1'000'000")).toBe(true);
    });

    test('highlights Python prefixed strings while rejecting contractions', () => {
        const code = "msg = f'hi {name}' + r'raw' # don't break";
        const tokens = tokenizeCode(code);
        expect(joinTokens(tokens)).toBe(code);
        const strs = tokensOfType(tokens, 'string').map((t) => t.value);
        expect(strs).toEqual(["'hi {name}'", "'raw'"]);
        expect(hasToken(tokens, 'comment', "# don't break")).toBe(true);
    });

    test('matches brackets with nesting depth and leaves unmatched brackets plain', () => {
        const code = 'fn({[x]}) + [1, 2) + } + ((';
        const tokens = tokenizeCode(code);
        expect(joinTokens(tokens)).toBe(code);
        const brackets = tokensOfType(tokens, 'bracket');
        expect(brackets.map((b) => [b.value, b.depth, b.pairId])).toEqual([
            ['(', 0, 3],
            ['{', 1, 2],
            ['[', 2, 1],
            [']', 2, 1],
            ['}', 1, 2],
            [')', 0, 3]
        ]);
    });

    test('highlights common keywords and operators with member lookback', () => {
        const code = 'const x = obj.delete ?? (a === b ? 1 : 2); fut.await';
        const tokens = tokenizeCode(code);
        expect(joinTokens(tokens)).toBe(code);
        expect(tokensOfType(tokens, 'keyword').map((t) => t.value)).toEqual(['const', 'await']);
        expect(tokensOfType(tokens, 'operator').map((t) => t.value)).toEqual([
            '=', '??', '===', '?', ':'
        ]);
    });
});

describe('failure modes, template safety, and performance', () => {
    test('aborts with plain text when maxIterations is exceeded', () => {
        const code = 'const x = 123 + "hello"; // comment';
        const tokens = tokenizeCode(code, { maxIterations: 5 });
        expect(tokens).toEqual([{ type: 'text', value: code }]);
    });

    test('handles empty, null, undefined, and non-string inputs safely', () => {
        expect(tokenizeCode('')).toEqual([]);
        expect(tokenizeCode(null)).toEqual([]);
        expect(tokenizeCode(undefined)).toEqual([]);
        expect(tokenizeCode(12345)).toEqual([]);
    });

    test('tokenizes 50KB input quickly without exceeding default iteration cap', () => {
        const unit = 'function add(a, b) { return a + b + 42; /* ok */ }\n';
        const large = unit.repeat(1050); // > 50KB
        const start = performance.now();
        const tokens = tokenizeCode(large);
        const elapsedMs = performance.now() - start;
        expect(joinTokens(tokens)).toBe(large);
        expect(tokensOfType(tokens, 'keyword').length).toBe(2100);
        expect(elapsedMs).toBeLessThan(200);
    });

    test('contains no literal double opening braces for Go text/template safety', () => {
        const src = readFileSync(new URL('./highlighter.js', import.meta.url), 'utf8');
        const testSrc = readFileSync(new URL('./highlighter.test.js', import.meta.url), 'utf8');
        const forbidden = '{' + '{';
        expect(src.includes(forbidden)).toBe(false);
        expect(testSrc.includes(forbidden)).toBe(false);
    });
});

describe('highlightCodeElement DOM rendering', () => {
    test('renders highlighted spans and toggles active bracket pair on hover', () => {
        const { doc, MockElement } = createMockDOM();
        const codeEl = new MockElement('code', doc);
        const raw = 'const x = (42 + "hi"); // ok';
        codeEl.textContent = raw;

        const applied = highlightCodeElement(codeEl);
        expect(applied).toBe(true);
        expect(codeEl.textContent).toBe(raw);

        const brackets = codeEl.querySelectorAll('.hl-bracket');
        expect(brackets.length).toBe(2);
        expect(brackets[0].dataset.bracketPair).toBe(brackets[1].dataset.bracketPair);

        codeEl.dispatchEvent('mouseover', { target: brackets[0] });
        expect(brackets[0].classList.contains('hl-bracket-active')).toBe(true);
        expect(brackets[1].classList.contains('hl-bracket-active')).toBe(true);

        // Moving directly to partner bracket keeps pair highlighted
        codeEl.dispatchEvent('mouseout', { target: brackets[0], relatedTarget: brackets[1] });
        expect(brackets[0].classList.contains('hl-bracket-active')).toBe(true);
        expect(brackets[1].classList.contains('hl-bracket-active')).toBe(true);

        codeEl.dispatchEvent('mouseout', { target: brackets[1], relatedTarget: null });
        expect(brackets[0].classList.contains('hl-bracket-active')).toBe(false);
        expect(brackets[1].classList.contains('hl-bracket-active')).toBe(false);
    });

    test('leaves plain-text-only code unmodified and returns false', () => {
        const { doc, MockElement } = createMockDOM();
        const codeEl = new MockElement('code', doc);
        codeEl.textContent = 'plain identifier words';
        expect(highlightCodeElement(codeEl)).toBe(false);
        expect(codeEl.children.length).toBe(1);
    });

    test('falls back to unmodified plain text if DOM mutation fails', () => {
        const { doc, MockElement } = createMockDOM();
        const brokenDoc = {
            ...doc,
            createElement: () => {
                throw new Error('DOM failure');
            }
        };
        const codeEl = new MockElement('code', brokenDoc);
        const raw = 'const a = 1;';
        codeEl.textContent = raw;

        expect(highlightCodeElement(codeEl)).toBe(false);
        expect(codeEl.textContent).toBe(raw);
    });
});

const TOP_20_LANGUAGE_SAMPLES = [
    {
        lang: 'JavaScript',
        valid: `// greet user\nexport async function greet(name) {\n  const count = 3;\n  return \`Hello, \${name}! (\${count})\`;\n}`,
        invalid: `const broken = "unterminated string\nfunction ( { [ ]`
    },
    {
        lang: 'TypeScript',
        valid: `interface User {\n  readonly id: number;\n  name: string;\n}\nconst u: User = { id: 1, name: "Ada" };`,
        invalid: `type Bad = interface { x = === ;`
    },
    {
        lang: 'Python',
        valid: `# compute sum\ndef total(items):\n    """Docstring"""\n    if len(items) == 0:\n        return 0\n    return sum(items) + 1_000`,
        invalid: `def broken(x\n    return '''unclosed triple`
    },
    {
        lang: 'Go',
        valid: `package main\n// main entry\nfunc main() {\n\tx := 42\n\tif x >= 10 {\n\t\treturn\n\t}\n}`,
        invalid: `func bad( { x := := "oops`
    },
    {
        lang: 'Rust',
        valid: `fn first<'a>(s: &'a str) -> usize {\n    let n: u64 = 0xff_u64;\n    if n > 0 { return s.len(); }\n    0\n}`,
        invalid: `fn bad<'a(x: &'a { let = 0b22;`
    },
    {
        lang: 'C',
        valid: `#include <stdio.h>\n/* sum */\nint main(void) {\n    int x = 0x10;\n    return x + 1;\n}`,
        invalid: `int main( { char c = 'unclosed; /* open`
    },
    {
        lang: 'C++',
        valid: `class Vec {\npublic:\n    int size() const { return 1'000; }\n}; // end`,
        invalid: `template<typename T class { ptr-> = ;`
    },
    {
        lang: 'Java',
        valid: `public class App {\n    public static void main(String[] args) {\n        final double pi = 3.14;\n    }\n}`,
        invalid: `public class { int x = "bad\\" ;;;`
    },
    {
        lang: 'C#',
        valid: `namespace Demo {\n    public async Task<int> Run() {\n        var ok = true;\n        return ok ? 1 : 0;\n    }\n}`,
        invalid: `public sealed class [ ] ) {`
    },
    {
        lang: 'PHP',
        valid: `<?php\n// loop\n$sum = 0;\nfor ($i = 0; $i < 5; $i++) {\n    $sum += $i;\n}\necho "done";`,
        invalid: `<?php function ( { $x = 'no end`
    },
    {
        lang: 'Ruby',
        valid: `# check value\ndef valid?(n)\n  return false if n <= 0\n  true\nend`,
        invalid: `def oops(\n  puts "bad\n  end end (`
    },
    {
        lang: 'Swift',
        valid: `struct Point {\n    let x: Int\n    func norm() -> Int {\n        return x * x\n    }\n}`,
        invalid: `func broken( -> { let x = 0x;`
    },
    {
        lang: 'Kotlin',
        valid: `fun add(a: Int, b: Int): Int {\n    val flag = null\n    return if (flag == null) a + b else 0\n}`,
        invalid: `fun (val x: = ) { "unclosed`
    },
    {
        lang: 'SQL',
        valid: `-- fetch active users\nSELECT id, name FROM users WHERE active = TRUE AND score >= 100 ORDER BY id LIMIT 10;`,
        invalid: `SELECT FROM WHERE ( ( 'unclosed`
    },
    {
        lang: 'Bash/Shell',
        valid: `#!/bin/sh\n# check file\nif [ -f "/etc/hosts" ]; then\n  echo "ok"\nfi`,
        invalid: `if [ "unclosed; then fi done (`
    },
    {
        lang: 'HTML/XML',
        valid: `<!-- banner -->\n<div class="card" data-count="42">Hello</div>`,
        invalid: `<!-- unclosed <div class="broken`
    },
    {
        lang: 'CSS',
        valid: `/* theme */\n.btn {\n    color: #ff00aa;\n    margin: 12px;\n}`,
        invalid: `.broken { color: #12345; margin: ; }`
    },
    {
        lang: 'JSON/YAML',
        valid: `{\n  "enabled": true,\n  "retries": 3,\n  "empty": null\n}\n# yaml note\nname: besedka`,
        invalid: `{ "bad": [1, 2, "unclosed }`
    },
    {
        lang: 'Lua',
        valid: `-- fibonacci\nlocal function fib(n)\n  if n <= 1 then return n end\n  return fib(n - 1) + fib(n - 2)\nend`,
        invalid: `local function ( if then end ]`
    },
    {
        lang: 'Zig/Scala',
        valid: `pub fn add(a: i32, b: i32) i32 {\n    return a + b;\n}\nobject Main { val xs = List(1, 2, 3) }`,
        invalid: `pub fn ( { val x = 0b; /* open`
    }
];

describe('Top 20 common languages (valid and invalid syntax)', () => {
    test('covers at least 20 distinct languages', () => {
        expect(TOP_20_LANGUAGE_SAMPLES.length).toBe(20);
        expect(COMMON_KEYWORDS.size).toBeGreaterThan(80);
    });

    for (const sample of TOP_20_LANGUAGE_SAMPLES) {
        test(`${sample.lang} valid snippet preserves text and highlights tokens`, () => {
            const tokens = tokenizeCode(sample.valid);
            expect(joinTokens(tokens)).toBe(sample.valid);
            expect(tokens.some((t) => t.type !== 'text')).toBe(true);
        });

        test(`${sample.lang} invalid snippet terminates cleanly and preserves text`, () => {
            const tokens = tokenizeCode(sample.invalid);
            expect(joinTokens(tokens)).toBe(sample.invalid);
            expect(tokens.length).toBeGreaterThan(0);
        });
    }
});
