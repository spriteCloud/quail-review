# Contributing to reviewqa

Thanks for considering a contribution.

## Before you open a PR

1. Read the [Contributor License Agreement](./CLA.md). The first time you
   open a PR, the `cla-assistant` bot will leave a comment with a one-line
   sign phrase; reply with that phrase to sign.
2. Run the test suites locally:
   - Go: `go test ./...`
   - Page: `cd web && npx playwright test`
3. Keep PRs focused. Bug fixes, small features, and doc fixes are all
   welcome — large refactors are best discussed in an issue first.

## Reporting issues

Open an issue at https://github.com/spriteCloud/reviewqa/issues with steps
to reproduce, the version (`reviewqa --version`), and what you expected.

## License

By contributing, you agree your work is released under the
[AGPL-3.0](./LICENSE) and that spriteCloud may also offer it under the
commercial license described in [`COMMERCIAL.md`](./COMMERCIAL.md). See
[`CLA.md`](./CLA.md) for the full terms.
