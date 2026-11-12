The trial-storage fixtures are the canonical manifests from the DE25 image
transition that exposed bootstrap processes retaining temporary storage:

- `trial-storage-old-manifest.json`: installed e412 software `a0e9e3c2…`,
  selected at generation 39 on the old base.
- `trial-storage-factory-manifest.json`: f607 factory software `ea5b37cc…`.
- `trial-storage-base.json`: the f607 image's actual base compatibility.

The regression verifies their complete manifest IDs and compatibility with the
real manifest implementation. Its child processes hold ordinary file descriptors
to model startup storage choices, and the production process controller retires
them. It does not mount FESDATA3, run SQLite or prove native agent inventory.
Those remain exact-artifact hardware checks after a rebuilt image boots.
