export default [
  {
    ignores: [
      "node_modules/**",
      "internal/assets/static/js/vendor/**",
      "playwright-report/**",
      "test-results/**",
    ],
  },
  {
    files: ["internal/assets/static/js/**/*.js"],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: "script",
    },
    rules: {
      "no-constant-binary-expression": "error",
      "no-redeclare": "error",
      "no-unreachable": "error",
      "no-unsafe-finally": "error",
    },
  },
  {
    files: ["tests/js/**/*.cjs"],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: "commonjs",
    },
    rules: {
      "no-constant-binary-expression": "error",
      "no-redeclare": "error",
      "no-unreachable": "error",
      "no-unsafe-finally": "error",
    },
  },
];
