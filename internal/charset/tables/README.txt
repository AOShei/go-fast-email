The tables of the double-byte code pages are read from this directory by
../dbcs.go: cp932.txt, cp936.txt, cp949.txt and cp950.txt, as written by

    python3 internal/charset/gen/gen_dbcs.py internal/charset/tables

They are the same files as go-fast-html internal/charset/cp93*.txt and
cp949.txt, cp950.txt at commit be5c679 (see NOTICE). Every file in this
directory is embedded in the binary (go:embed in ../dbcs.go).

gb18030-2.txt, gb18030-4.txt, jis0208.txt and jis0212.txt are read by
../cjk.go for GB18030, EUC-JP and ISO-2022-JP, as written by

    python3 internal/charset/gen/gen_cjk_extra.py internal/charset/tables

whose header says what each holds.
