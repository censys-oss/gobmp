gobmp
=====

A Go package for reading and writing BMP image files.


Installation
------------

Import as: `github.com/censys-oss/gobmp` and `go mod tidy`


Documentation
-------------

Gobmp is designed to work the same as Go's standard
[image modules](http://golang.org/pkg/image/). Importing it will automatically
cause the image.Decode function to support reading BMP files.

The documentation may be read online at
[GoDoc](http://godoc.org/github.com/censys-oss/gobmp)

Usage
-----

```go
reader, err := os.Open("example.bmp")
if err != nil {
        log.Fatal(err)
}
defer reader.Close()

// To decode and return image
image, err := Decode(r)
if err != nil {
        log.Fatal(err)
}

// To decode, with memory limits
image, err := Decode(r, WithMemoryLimit(10_000_000))
if err != nil {
        log.Fatal(err)
}
```


Dependencies
------------

Does not import external dependencies

Status
------

The decoder supports almost all types of BMP images.

By default, the encoder will write a 24-bit RGB image, or a 1-, 4-, or 8-bit
paletted image. Support for 32-bit RGBA images can optionally be enabled.
Writing compressed images is not supported.

This library is vetted to handle decoding without panicking and can return an error if allocation hits a defined limit.

See `todo.md` for what could be worked on.


License
-------

Gobmp is distributed under an MIT-style license. Refer to the LICENSE.txt
file.

Copyright &copy; 2025 Censys
Copyright &copy; 2012-2015 Jason Summers
<[jason1@pobox.com](mailto:jason1@pobox.com)>
