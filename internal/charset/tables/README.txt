The tables of the double-byte code pages are read from this directory by
../dbcs.go: cp932.txt, cp936.txt, cp949.txt and cp950.txt, as written by

    python3 internal/charset/gen/gen_dbcs.py internal/charset/tables

They are the same files as go-fast-html internal/charset/cp93*.txt and
cp949.txt, cp950.txt at commit be5c679 (see NOTICE). A code page whose
file is missing is not decoded.
