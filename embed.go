package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"flag"
	"fmt"
	"go/build"
	"hash"
	"io"
	"os"
	"path/filepath"
	"unicode"
	"unicode/utf8"
)

func usage() {
	app := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "Usage:\n  %s [OPTIONS] FILE...\n\n", app)
	fmt.Fprintf(os.Stderr, "By default %s reads the input files and writes their\n", app)
	fmt.Fprintf(os.Stderr, "contents as embedded data in one Go file per input\n")
	fmt.Fprintf(os.Stderr, "file, by appending .go to the filename. Specifying -o\n")
	fmt.Fprintf(os.Stderr, "overrides this by writing all files to a single output\n")
	fmt.Fprintf(os.Stderr, "with the given name. %s attempts to detect the package\n", app)
	fmt.Fprintf(os.Stderr, "name but it can be specified with -package.\n\n")
	flag.PrintDefaults()
	os.Exit(2)
}

var (
	pkg      = flag.String("package", "", "Package name in output file(s)")
	output   = flag.String("o", "", "Output all data to this file")
	compress = flag.Bool("gzip", false, "Compress data with gzip before embedding")
	sha      = flag.Bool("sha1", false, "Also embed SHA1 hash of data")
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		usage()
	}

	// Package name

	if *pkg != "" {
		*pkg = sanitise(*pkg)
	} else {
		dir := "."
		if *output != "" {
			dir = filepath.Dir(*output)
		}

		p, err := build.ImportDir(dir, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to determine package name: %v\n", err)
			os.Exit(1)
		}

		if p.Name == "" || p.Name == "." {
			fmt.Fprintf(os.Stderr, "Failed to determine package name\n")
			os.Exit(1)
		}

		*pkg = p.Name
	}

	// Output

	var (
		dst *os.File
		err error
	)

	if *output != "" {
		dst, err = os.Create(*output)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		if err = WritePackage(dst); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write package statement: %v\n", err)
			dst.Close()
			os.Exit(1)
		}
	}

	// Inputs

	for _, name := range args {
		src, err := os.Open(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}

		if dst == nil {
			dst, err = os.Create(filepath.Base(name) + ".go")
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				src.Close()
				continue
			}

			if err = WritePackage(dst); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to write package statement: %v\n", err)
				dst.Close()
				src.Close()
				os.Exit(1)
			}
		}

		if err = Embed(dst, src, name); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to embed data: %v\n", err)
			dst.Close()
			src.Close()
			os.Exit(1)
		}

		if err = src.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close source: %v\n", err)
			dst.Close()
			os.Exit(1)
		}

		if *output == "" {
			if err = dst.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to close output: %v\n", err)
				os.Exit(1)
			}

			dst = nil
		}
	}
}

func WritePackage(dst io.Writer) error {
	_, err := fmt.Fprintf(dst, "// MACHINE GENERATED - DO NOT EDIT //\n\npackage %s\n", *pkg)
	return err
}

func Embed(dst io.Writer, src io.Reader, name string) (err error) {
	if *compress {
		var wc io.WriteCloser
		wc, err = gzip.NewWriterLevel(dst, gzip.BestCompression)
		if err != nil {
			return err
		}

		defer func() {
			e := wc.Close()
			if err == nil && e != nil {
				err = e
			}
		}()

		dst = wc
	}

	var hasher hash.Hash
	if *sha {
		hasher = sha1.New()
		dst = io.MultiWriter(dst, hasher)
	}

	const BUF_SIZE = 12

	var n int
	var sanitised = sanitise(name)
	var buf [BUF_SIZE]byte

	_, err = fmt.Fprintf(dst, "\n// %s\nvar %s = []byte{\n", name, sanitised)
	if err != nil {
		return err
	}

	for {
		n, err = io.ReadFull(src, buf[:])
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return err
		}

		if n == 0 {
			break
		}

		var w bytes.Buffer
		for i := 0; i < n; i++ {
			fmt.Fprintf(&w, "0x%02x, ", buf[i])
		}

		data := w.String()
		_, err = fmt.Fprintf(dst, "\t%s\n", data[:len(data)-1])
		if err != nil {
			return err
		}

		if n != BUF_SIZE {
			break
		}
	}

	_, err = fmt.Fprintf(dst, "}\n")
	if err != nil {
		return err
	}

	if hasher != nil {
		sum := hasher.Sum(nil)
		_, err = fmt.Fprintf(dst, "\n// SHA1 hash of %s\nvar %s_SHA1 = []byte{\n", name, sanitised)
		if err != nil {
			return err
		}

		for len(sum) > 0 {
			n := len(sum)
			if n > BUF_SIZE {
				n = BUF_SIZE
			}

			var w bytes.Buffer
			for i := 0; i < n; i++ {
				fmt.Fprintf(&w, "0x%02x, ", sum[i])
			}

			data := w.String()
			_, err = fmt.Fprintf(dst, "\t%s\n", data[:len(data)-1])
			if err != nil {
				return err
			}

			sum = sum[n:]
		}

		_, err = fmt.Fprintf(dst, "}\n")
		if err != nil {
			return err
		}
	}

	return err
}

func sanitise(name string) string {
	var buf bytes.Buffer
	var first = true

	name = filepath.Base(name)
	for len(name) > 0 {
		r, n := utf8.DecodeRuneInString(name)
		if unicode.IsLetter(r) || (!first && unicode.IsNumber(r)) {
			first = false
			buf.WriteRune(r)
		} else {
			buf.WriteByte('_')
		}

		name = name[n:]
	}

	if buf.Len() == 0 {
		buf.WriteByte('_')
	}

	return buf.String()
}
