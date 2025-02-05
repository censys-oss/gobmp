# BMP File Format

This document describes the structure of the BMP format (commonly stored in `.bmp` files). A BMP file is a simple image format that stores images in a lightly compressed fashion.

## File Structure

A BMP file has four main sections (with gaps between sections):

1. The **bitmap file header** (`BITMAPFILEHEADER`), which specifies basic file properties. 14 bytes.
2. The **DIB header** (`BITMAPINFOHEADER` or others), which describes the image dimensions, color depth, and compression. Varies, but typically 40 or more bytes.
3. An optional **color palette**, used if the image is indexed (e.g., 1, 4, or 8 bits per pixel).
4. The **pixel data**, which contains the actual image, potentially with row padding.

Conceptually, the structure looks like this:

```c
typedef struct BITMAPFILEHEADER {
    WORD  bfType;
    DWORD bfSize;
    WORD  bfReserved1;
    WORD  bfReserved2;
    DWORD bfOffBits;
} BITMAPFILEHEADER;
```

```c
typedef struct BITMAPINFOHEADER {
    DWORD biSize;
    LONG  biWidth;
    LONG  biHeight;
    WORD  biPlanes;
    WORD  biBitCount;
    DWORD biCompression;
    DWORD biSizeImage;
    LONG  biXPelsPerMeter;
    LONG  biYPelsPerMeter;
    DWORD biClrUsed;
    DWORD biClrImportant;
} BITMAPINFOHEADER;
```
... (maybe) color palette + gap
image data
(maybe) gap + ICC color profile

### Bitmap File Header (BITMAPFILEHEADER)

The **BITMAPFILEHEADER** is a fixed-length header occupying 14 bytes:

| Offset | Size | Field         | Description                                      |
|--------:|----:|--------------|--------------------------------------------------|
| 0       | 2   | `bfType`      | File signature   |
| 2       | 4   | `bfSize`      | Total file size in bytes                        |
| 6       | 2   | `bfReserved1` | Reserved. Must be 0                            |
| 8       | 2   | `bfReserved2` | Reserved. Must be 0                            |
| 10      | 4   | `bfOffBits`   | Offset to the start of pixel data               |

- **bfType**: Magic `BM` to signify this file is BMP
- **bfSize**: Total file size
- **bfOffBits**: Where pixel data starts

### DIB Header (BITMAPINFOHEADER)

The **DIB header** follows the file header and varies in size. The most common, `BITMAPINFOHEADER`, is 40 bytes:

| Offset | Size | Field          | Description                                             |
|--------:|----:|---------------|---------------------------------------------------------|
| 0       | 4   | `biSize`       | Size of the header (usually 40 bytes), crucial for determining which header we have                  |
| 4       | 4   | `biWidth`      | Width of the image in pixels                          |
| 8       | 4   | `biHeight`     | Height in pixels |
| 12      | 2   | `biPlanes`     | Number of planes, must be 1                           |
| 14      | 2   | `biBitCount`   | Bits per pixel (1, 4, 8, 16, 24, or 32)               |
| 16      | 4   | `biCompression`| Compression type (`BI_RGB` for none)                  |
| 20      | 4   | `biSizeImage`  | Size of image data in bytes (0 if uncompressed)       |
| 24      | 4   | `biXPelsPerMeter` | Horizontal resolution (pixels per meter)        |
| 28      | 4   | `biYPelsPerMeter` | Vertical resolution (pixels per meter)          |
| 32      | 4   | `biClrUsed`    | Number of colors used in the palette                 |
| 36      | 4   | `biClrImportant` | Important colors (0 means all)                 |

### Color Palette

The **color palette** is used for indexed color images (<8 bits per pixel). Each entry is 3 or 4 bytes, either RGB or ARGB format.

The number of entries is defined by `biClrUsed` or inferred from `biBitCount`.

### Pixel Data

Following the headers and palette, the **pixel data** section stores the image. 

- **Storage Order**:
  - Bottom-up (most BMPs): The first row stored is the last row of the image.
  - Top-down (`biHeight` negative): The first row stored is the top row.

- **Padding**:
  - Each row’s size in bytes is padded to the nearest multiple of 4.

### Example: Row Padding in a 24-bit BMP

For a 3-pixel wide image:
- 24-bit BMP uses 3 bytes per pixel, so one row is 9 bytes.
- To align to a 4-byte boundary, the row is padded to 12 bytes.
- **Padding:** 3 extra bytes (typically zeros) are added at the end of each row.

## Compression Methods

The BMP format supports several compression methods, defined by `biCompression`:

| Value | Name        | Description                                    |
|------:|------------|------------------------------------------------|
| 0     | `BI_RGB`   | No compression                                |
| 1     | `BI_RLE8`  | Run-Length Encoding for 8-bit images         |
| 2     | `BI_RLE4`  | Run-Length Encoding for 4-bit images         |
| 3     | `BI_BITFIELDS` | Used in 16/32-bit bpp BMPs with RGB color masks |

...among (unimplemented) others

## References

- [wikipedia](https://en.wikipedia.org/wiki/BMP_file_format)
- [bitmap info header](https://learn.microsoft.com/en-us/windows/win32/wmdm/-bitmapinfoheader)
- [ms bmp docs](https://learn.microsoft.com/en-us/windows/win32/gdi/bitmap-storage)
- [compression](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-wmf/4e588f70-bd92-4a6f-b77f-35d0feaf7a57)