(function () {
    'use strict';

    const THEME_STORAGE_KEY = 'besedka.theme';
    const FONT_SCALE_STORAGE_KEY = 'besedka.fontScale';
    const VALID_THEME_PREFERENCES = new Set(['dark', 'light', 'system']);
    const FONT_SCALE_MIN = 80;
    const FONT_SCALE_MAX = 150;
    const FONT_SCALE_STEP = 10;
    const FONT_SCALE_DEFAULT = 100;
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');

    let themePreference = readThemePreference();
    let fontScale = readFontScale();

    function normalizeThemePreference(value) {
        return VALID_THEME_PREFERENCES.has(value) ? value : 'dark';
    }

    function isValidFontScale(value) {
        const parsed = Number(value);
        return Number.isInteger(parsed)
            && parsed >= FONT_SCALE_MIN
            && parsed <= FONT_SCALE_MAX
            && parsed % FONT_SCALE_STEP === 0;
    }

    function normalizeFontScale(value) {
        return isValidFontScale(value) ? Number(value) : FONT_SCALE_DEFAULT;
    }

    function readThemePreference() {
        try {
            return normalizeThemePreference(localStorage.getItem(THEME_STORAGE_KEY));
        } catch {
            return 'dark';
        }
    }

    function readFontScale() {
        try {
            return normalizeFontScale(localStorage.getItem(FONT_SCALE_STORAGE_KEY));
        } catch {
            return FONT_SCALE_DEFAULT;
        }
    }

    function writePreference(key, value) {
        try {
            localStorage.setItem(key, String(value));
        } catch {
            // Appearance changes still work for this page when storage is unavailable.
        }
    }

    function resolveTheme(value) {
        if (value === 'system') return mediaQuery.matches ? 'dark' : 'light';
        return value;
    }

    function appearanceDetail(changedProperty) {
        return {
            changedProperty,
            themePreference,
            resolvedTheme: resolveTheme(themePreference),
            fontScale
        };
    }

    function applyTheme(notify) {
        const resolvedTheme = resolveTheme(themePreference);
        const root = document.documentElement;
        root.dataset.theme = resolvedTheme;
        root.dataset.themePreference = themePreference;
        root.style.colorScheme = resolvedTheme;

        const themeColor = resolvedTheme === 'dark' ? '#0f1117' : '#f6f8fa';
        document.querySelectorAll('meta[name="theme-color"]').forEach((meta) => {
            meta.setAttribute('content', themeColor);
        });

        const statusBar = document.querySelector('meta[name="apple-mobile-web-app-status-bar-style"]');
        if (statusBar) {
            statusBar.setAttribute('content', resolvedTheme === 'dark' ? 'black-translucent' : 'default');
        }

        if (notify) {
            window.dispatchEvent(new CustomEvent('besedka-appearance-change', {
                detail: appearanceDetail('theme')
            }));
        }
    }

    function applyFontScale(notify) {
        const root = document.documentElement;
        root.style.setProperty('--app-font-size', `${fontScale}%`);
        root.dataset.fontScale = String(fontScale);

        if (notify) {
            window.dispatchEvent(new CustomEvent('besedka-appearance-change', {
                detail: appearanceDetail('fontScale')
            }));
        }
    }

    function setThemePreference(value, options = {}) {
        const nextPreference = normalizeThemePreference(value);
        const changed = themePreference !== nextPreference;
        themePreference = nextPreference;
        if (options.persist !== false) writePreference(THEME_STORAGE_KEY, themePreference);
        applyTheme(changed || options.force === true);
        return themePreference;
    }

    function setFontScale(value, options = {}) {
        const nextScale = normalizeFontScale(value);
        const changed = fontScale !== nextScale;
        fontScale = nextScale;
        if (options.persist !== false) writePreference(FONT_SCALE_STORAGE_KEY, fontScale);
        applyFontScale(changed || options.force === true);
        return fontScale;
    }

    const handleSystemThemeChange = () => {
        if (themePreference === 'system') applyTheme(true);
    };

    const handleStoredAppearanceChange = (event) => {
        if (event.key === THEME_STORAGE_KEY) {
            const nextPreference = normalizeThemePreference(event.newValue);
            const changed = themePreference !== nextPreference;
            themePreference = nextPreference;
            applyTheme(changed);
        } else if (event.key === FONT_SCALE_STORAGE_KEY) {
            const nextScale = normalizeFontScale(event.newValue);
            const changed = fontScale !== nextScale;
            fontScale = nextScale;
            applyFontScale(changed);
        }
    };

    if (typeof mediaQuery.addEventListener === 'function') {
        mediaQuery.addEventListener('change', handleSystemThemeChange);
    } else if (typeof mediaQuery.addListener === 'function') {
        mediaQuery.addListener(handleSystemThemeChange);
    }
    window.addEventListener('storage', handleStoredAppearanceChange);

    window.besedkaAppearance = {
        fontScale: {
            min: FONT_SCALE_MIN,
            max: FONT_SCALE_MAX,
            step: FONT_SCALE_STEP,
            defaultValue: FONT_SCALE_DEFAULT
        },
        getThemePreference: () => themePreference,
        getResolvedTheme: () => resolveTheme(themePreference),
        getFontScale: () => fontScale,
        isValidThemePreference: (value) => VALID_THEME_PREFERENCES.has(value),
        isValidFontScale,
        setThemePreference,
        setFontScale
    };

    applyTheme(false);
    applyFontScale(false);
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', () => {
            applyTheme(false);
            applyFontScale(false);
        }, { once: true });
    }
})();
