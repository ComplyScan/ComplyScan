# Pinned public-repository study

This directory contains provenance and human labels, not third-party source code. The manual benchmark downloads three MIT-licensed public repositories at exact commits and evaluates them with the same deterministic technical-evidence engine used by the CLI.

The repositories were selected to provide a larger Go AI runtime, a compact Python evaluation suite, and a large multi-language AI evaluation and red-team project. `sources.json` records the exact URLs, revisions, licence identifiers, and licence-file paths. `manifest.json` records candidate-level labels and acceptance thresholds for technical evidence only.

Run the study with network access:

```sh
./scripts/evaluate-external-repositories.sh
```

To reuse already checked-out pinned repositories, place them in one directory using the IDs from `sources.json` and run:

```sh
./scripts/evaluate-external-repositories.sh --workspace /path/to/checkouts
```

Add `--format json` for a source-free machine-readable result. The runner verifies every checkout's exact commit and licence-file presence before scanning. It exits `0` when thresholds pass, `1` for a metric failure, and `2` for provenance, checkout, or execution errors. The networked study is deliberately not a CI requirement.

## Baseline review

For technical pack `0.1.1`, the reviewed baseline has 13 true-positive candidates, four false-positive candidates, and no false negatives: 76.5% candidate precision and 100% recall across the labelled paths. All eight expected per-repository language detections are present. Context anchors and relationships are not labelled in this external study; those are enforced by the checked-in synthetic benchmark.

The four deliberately unlabelled results are documentation or generic-test signals in Promptfoo: two interactive website pages about AI safety/security and two source/test references whose nearby words resemble a technical control without implementing that control. They remain visible as false positives instead of being hidden by a path-wide documentation exclusion, because user-facing source can itself be valid evidence for other objectives. Optional semantic review is the intended next filtering layer.

These labels say only whether a file is a reasonable candidate for human technical review. They do not state that a repository, product, or control complies with the EU AI Act. Re-review the complete candidate set before changing a pinned revision, pack version, keyword boundary, or threshold.
