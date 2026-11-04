# Native entry inflater

`puff.c` and `puff.h` are unmodified Mark Adler puff 2.3 sources, selected from
zlib v1.3.1 at the revision and SHA-256 values in `upstream.json`. The zlib
license is retained in `puff.h`.

The entry uses this allocation-free raw DEFLATE decoder inside a strict gzip
wrapper. A private `setjmp.h` binds its error path to GCC compiler builtins;
no C runtime or Linux is linked. FES supplies that shim; it is not upstream code.
The decoder, wrapper and ELF planner are tested together before native boot.
