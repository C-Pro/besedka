const globals = require("globals");
const js = require("@eslint/js");

module.exports = [
    { ignores: ["js/qrcode.min.js"] },
    js.configs.recommended,
    {
        languageOptions: {
            ecmaVersion: "latest",
            sourceType: "module",
            globals: {
                ...globals.browser,
                ...globals.jest
            }
        },
        rules: {
        }
    }
];
