// @mention autocomplete for the message input.
//
// Detects an "@token" at the caret, shows a filtered username dropdown, and
// inserts "@userName " on selection. Designed to behave on mobile: items are
// confirmed on `pointerdown` with preventDefault so tapping does not blur the
// textarea (which would dismiss the on-screen keyboard), and the list is
// positioned within the visual viewport so it stays above the keyboard.

const MAX_RESULTS = 8;
// Token at the caret: optional boundary, '@', then username-charset chars.
const tokenRegex = /(^|[^a-zA-Z0-9._-])@([a-zA-Z0-9._-]*)$/;

export function attachMentionAutocomplete(inputEl, store) {
    let open = false;
    let matches = [];
    let selectedIndex = 0;
    let tokenStart = 0;
    let query = '';

    const dropdown = document.createElement('div');
    dropdown.className = 'mention-autocomplete';
    dropdown.style.display = 'none';
    dropdown.style.position = 'fixed';
    document.body.appendChild(dropdown);

    const isOpen = () => open;

    const reposition = () => {
        if (!open) return;
        const rect = inputEl.getBoundingClientRect();
        const gap = 4;
        // Constrain height to the space above the input so the list never hides
        // behind the on-screen keyboard or runs off the top of the screen.
        const available = Math.max(80, rect.top - gap - 8);
        dropdown.style.maxHeight = `${available}px`;
        dropdown.style.left = `${rect.left}px`;
        dropdown.style.width = `${rect.width}px`;
        // Measure after sizing, then anchor the bottom just above the input.
        const height = dropdown.offsetHeight;
        dropdown.style.top = `${Math.max(8, rect.top - height - gap)}px`;
    };

    const render = () => {
        dropdown.replaceChildren();
        matches.forEach((user, i) => {
            const item = document.createElement('div');
            item.className = i === selectedIndex ? 'mention-item active' : 'mention-item';
            item.dataset.username = user.userName;

            const name = document.createElement('span');
            name.className = 'mention-display';
            name.textContent = user.displayName || user.userName;

            const handle = document.createElement('span');
            handle.className = 'mention-username';
            handle.textContent = `@${user.userName}`;

            item.appendChild(name);
            item.appendChild(handle);

            // pointerdown (not click) + preventDefault keeps focus on the
            // textarea so the mobile keyboard stays up through the selection.
            item.addEventListener('pointerdown', (e) => {
                e.preventDefault();
                confirm(i);
            });
            dropdown.appendChild(item);
        });
        reposition();
    };

    const openDropdown = () => {
        if (!open) {
            open = true;
            dropdown.style.display = 'block';
            window.addEventListener('scroll', reposition, true);
            window.addEventListener('resize', reposition);
            if (window.visualViewport) {
                window.visualViewport.addEventListener('resize', reposition);
                window.visualViewport.addEventListener('scroll', reposition);
            }
        }
        render();
    };

    const close = () => {
        if (!open) return;
        open = false;
        dropdown.style.display = 'none';
        dropdown.replaceChildren();
        window.removeEventListener('scroll', reposition, true);
        window.removeEventListener('resize', reposition);
        if (window.visualViewport) {
            window.visualViewport.removeEventListener('resize', reposition);
            window.visualViewport.removeEventListener('scroll', reposition);
        }
    };

    const confirm = (index) => {
        const user = matches[index];
        if (!user) return;
        const value = inputEl.value;
        const before = value.slice(0, tokenStart);
        const after = value.slice(tokenStart + 1 + query.length);
        const insert = `@${user.userName} `;
        inputEl.value = before + insert + after;
        const caret = before.length + insert.length;
        inputEl.setSelectionRange(caret, caret);
        close();
        inputEl.focus();
        // Re-run the input handlers (auto-resize, and our own detection which
        // will see no token at the caret and stay closed).
        inputEl.dispatchEvent(new Event('input', { bubbles: true }));
    };

    const update = () => {
        const caret = inputEl.selectionStart ?? inputEl.value.length;
        const before = inputEl.value.slice(0, caret);
        const m = before.match(tokenRegex);
        if (!m) {
            close();
            return;
        }
        query = m[2];
        tokenStart = caret - query.length - 1; // index of '@'
        const q = query.toLowerCase();
        const selfId = store.state.currentUser?.id;
        matches = (store.state.users || [])
            .filter((u) => u.userName && u.id !== selfId && u.userName.toLowerCase().startsWith(q))
            .slice(0, MAX_RESULTS);
        if (matches.length === 0) {
            close();
            return;
        }
        selectedIndex = 0;
        openDropdown();
    };

    const handleKeydown = (e) => {
        if (!open) return false;
        switch (e.key) {
            case 'ArrowDown':
                selectedIndex = (selectedIndex + 1) % matches.length;
                render();
                e.preventDefault();
                return true;
            case 'ArrowUp':
                selectedIndex = (selectedIndex - 1 + matches.length) % matches.length;
                render();
                e.preventDefault();
                return true;
            case 'Enter':
            case 'Tab':
                confirm(selectedIndex);
                e.preventDefault();
                return true;
            case 'Escape':
                close();
                e.preventDefault();
                return true;
            default:
                return false;
        }
    };

    inputEl.addEventListener('input', update);
    inputEl.addEventListener('click', update);
    inputEl.addEventListener('keyup', (e) => {
        // Re-evaluate when the caret moves without changing text.
        if (['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) {
            update();
        }
    });
    inputEl.addEventListener('blur', () => {
        // Delay so a pointerdown selection still registers.
        setTimeout(close, 100);
    });

    // Close when the user switches chats, or switches mobile tabs. A swipe
    // changes mobileActiveTab without changing activeChatId, and the dropdown
    // is position:fixed on <body>, so it would otherwise hang over the new tab
    // (and keep its viewport listeners running against a hidden input).
    let lastChatId = store.state.activeChatId;
    let lastTab = store.state.mobileActiveTab;
    store.subscribe((state) => {
        if (state.activeChatId !== lastChatId || state.mobileActiveTab !== lastTab) {
            lastChatId = state.activeChatId;
            lastTab = state.mobileActiveTab;
            if (open) close();
        }
    });

    return { isOpen, handleKeydown, close };
}
