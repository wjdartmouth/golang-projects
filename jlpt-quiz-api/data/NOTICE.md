# NOTICE — OpenJLPT data (adapted)

The JSON files in this directory are an **adapted excerpt** of the
[OpenJLPT](https://github.com/evanclan/OpenJLPT) dataset. They contain
vocabulary, kanji, and grammar used by the JLPT quiz service.

**License:** [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/).
These adapted data files remain licensed under CC BY-SA 4.0 (ShareAlike).
The surrounding application code is MIT-licensed (see the repository root
`LICENSE`); that license does not apply to the files in this directory.

OpenJLPT is itself a derived work. If you use this data, you must give
appropriate credit to OpenJLPT and its upstream sources, provide a link to
CC BY-SA 4.0, and distribute any further derivative dataset under
CC BY-SA 4.0.

The credit line recommended by OpenJLPT:

> Contains data from [OpenJLPT](https://github.com/evanclan/OpenJLPT) (CC BY-SA 4.0), which uses
> JMdict and KANJIDIC2 (EDRDG), Jonathan Waller's JLPT lists, and Tatoeba.

## Upstream sources (from OpenJLPT's NOTICE)

The following table and notes are reproduced from
[OpenJLPT's NOTICE.md](https://github.com/evanclan/OpenJLPT/blob/main/NOTICE.md)
so attribution is not invented here.

| Source | Used for | License | Link |
|---|---|---|---|
| **Jonathan Waller's JLPT Resources** (tanos.co.uk) | The **N5–N1 level assignments** for vocabulary and kanji, plus the English glosses for vocabulary. | CC BY | https://www.tanos.co.uk/jlpt/ |
| **JMdict** — Electronic Dictionary Research and Development Group (EDRDG) | Vocabulary `jmdict_id` and `pos`, verification and repair of readings, spellings and truncated glosses | CC BY-SA 4.0 | https://www.edrdg.org/jmdict/j_jmdict.html |
| **KANJIDIC2** — EDRDG | Kanji readings, meanings, stroke counts, grade, frequency, radical and name readings | CC BY-SA 4.0 | https://www.edrdg.org/wiki/KANJIDIC_Project.html |
| **Tatoeba** | Vocabulary example sentences (Japanese + English) | CC BY 2.0 FR | https://tatoeba.org |
| **OpenJLPT contributors** | Grammar points (explanations and example sentences) | CC BY-SA 4.0 | https://github.com/evanclan/OpenJLPT |

The EDRDG files (JMdict, KANJIDIC2) are the property of the Electronic Dictionary Research and
Development Group, and are used in conformance with the Group's
[licence](https://www.edrdg.org/edrdg/licence.html).

Example sentences from [Tatoeba](https://tatoeba.org) are licensed
[CC BY 2.0 FR](https://creativecommons.org/licenses/by/2.0/fr/).

## Important note on JLPT levels

The Japan Foundation / JLPT organisation does **not** publish official N5–N1 vocabulary, kanji
or grammar lists. Vocabulary and kanji levels come from **Jonathan Waller's community-standard
lists**, which are widely used and reliable, but they are *unofficial approximations* of the
real (undisclosed) test content. Grammar levels follow the consensus of common JLPT preparation
materials. (Quoted in substance from OpenJLPT's NOTICE.)

## How this copy was slimmed

Only fields needed by the quiz are kept: vocabulary entries store `id`, `word`,
`reading`, `romaji`, `meanings`, `level`, `pos`, and one example
(`ja`, `furigana`, `en`); kanji entries store `character`, `level`, `strokes`,
`onyomi`, `kunyomi`, `meanings`, and up to eight sample words; grammar entries
store `id`, `pattern`, `romaji`, `level`, `meaning`, `formation`, `notes`, and
up to three examples. To regenerate from upstream, run
`jlpt-quiz-api/scripts/refresh-openjlpt.sh`.
