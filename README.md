# embed

Embed is a simple tool for embedding content in Go source.

Embed reads the input files and writes their contents as embedded data
in one Go file per input file, by appending .go to the filename.
Specifying -o overrides this by writing all files to a single output
with the given name. Embed attempts to detect the package name but
it can be specified with -package.

Example:

```bash
$ embed -o content.go -gzip -sha1 content/index.html content/style.css
```
