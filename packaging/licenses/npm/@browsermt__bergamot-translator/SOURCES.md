# Licences for @browsermt/bergamot-translator

The npm package (0.4.9, MPL-2.0) ships without licence files. Its WebAssembly
build compiles in Marian and the libraries below, so their licences are
vendored here and included in THIRD_PARTY_NOTICES.md by `make notices`.

| File | Component | Source |
|---|---|---|
| bergamot-translator.txt | bergamot-translator (MPL-2.0) | github.com/browsermt/bergamot-translator `LICENSE` |
| marian.md | Marian NMT (MIT) | github.com/browsermt/marian-dev @2781d735 `LICENSE.md` |
| ssplit-cpp.md | ssplit-cpp (Apache-2.0 / CC BY 4.0) | github.com/browsermt/ssplit-cpp @a311f986 `LICENSE.md` |
| pcre2.md | PCRE2 (BSD-3-Clause with exception), used by ssplit-cpp | github.com/PCRE2Project/pcre2 `LICENCE.md` |
| intgemm.txt | intgemm (MIT) | github.com/kpu/intgemm @f7401513 `LICENSE` |
| sentencepiece.txt | SentencePiece (Apache-2.0) | github.com/browsermt/sentencepiece @ae41b774 `LICENSE` |
| sentencepiece-*.txt | its absl, darts_clone, esaxx and protobuf-lite | the same commit, `third_party/*/LICENSE` |
| CLI.txt, cnpy.txt, pathie-cpp.txt, phf.txt, spdlog.txt, yaml-cpp.txt, zstr.txt | Marian's third-party libraries | marian-dev @2781d735 `src/3rd_party/*/LICENSE` |
| half_float.txt | umHalf (BSD-3-Clause) | marian-dev `src/3rd_party/half_float/umHalf.h` header |
| zlib.txt | zlib | marian-dev `src/3rd_party/zlib/README` copyright notice |

The translation models aren't part of SheetDiver: they're downloaded on
request from Mozilla (MPL-2.0) and stay in the user's library.
