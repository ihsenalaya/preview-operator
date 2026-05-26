# Corrections applied

This version repositions the paper around post-teardown evidence provenance rather than RCA superiority.

Main changes:
- Rewrote the title and abstract around the evidence/provenance substrate.
- Reframed contributions to emphasize post-teardown artifact, diagnostic utility, and baseline comparability.
- Rewrote the research questions: hallucination is now exploratory, not a primary RQ.
- Clarified that 500/500 survival is expected from cluster-scoped storage; the experimental claim is the end-to-end capture and persistence workflow.
- Reframed RQ2 as diagnostic utility and limits, with explicit negative interpretation for weak RCA scenarios.
- Added an external-baseline comparability table.
- Explicitly noted that B0's 43% multi-app result is a qualifying/negative result, not evidence of superiority.
- Added threats on baseline comparability and single-cluster environmental validity.
- Updated future work with symmetric post-teardown baselines and logistic mixed-effects modeling.
- Rewrote the conclusion to avoid overclaiming RCA performance.

## Layout corrections applied in this pass
- Fixed the broken baseline-comparability table that overflowed on page 9 by converting it to a two-column `table*` and using controlled `tabularx` column widths.
- Resized the evidence-ladder tables to avoid overfull columns in ACM two-column layout.
- Added safer breakable artifact-path formatting for long analysis paths.
- Replaced raw section-symbol characters with LaTeX `\S` references.
- Increased `\headheight` to remove the ACM/fancyhdr header warning.
- Added figure descriptions for ACM accessibility warnings.
- Recompiled successfully to `article.pdf` and visually checked the repaired page 9.
