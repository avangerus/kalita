\# Workspace Conventions



\## Purpose

This document defines the fixed artifact structure for all AI-assisted development work in this repository.



Agents must follow this structure and must not invent alternative directories or filenames unless explicitly requested.



\## Standard Directories



\### /plans

Planning and analysis artifacts.



Files:

\- /plans/repo\_index.md

\- /plans/architecture\_review.md

\- /plans/IMP-CR-XXX.md

\- /plans/BUG-XXX-debug-report.md



\### /changes

Requests and implementation/fix logs.



Files:

\- /changes/CR-XXX-feature-name.md

\- /changes/CR-XXX-implementation-log.md

\- /changes/BUG-XXX-title.md

\- /changes/BUG-XXX-fix-log.md



\### /reviews

Code review outputs.



Files:

\- /reviews/CR-XXX-code-review.md

\- /reviews/BUG-XXX-review.md



\### /tests

Test execution and validation outputs.



Files:

\- /tests/CR-XXX-test-report.md

\- /tests/BUG-XXX-test-report.md



\### /docs

Reference documentation and workflow guides.



Files:

\- /docs/AI\_PIPELINE\_TEMPLATES.md

\- /docs/WORKSPACE\_CONVENTIONS.md



\## Rules



1\. Do not create new top-level directories for planning or review artifacts.

2\. Use the standard filenames whenever possible.

3\. If an artifact already exists, update it instead of duplicating it.

4\. If a change request exists, all related implementation, review, and test files must reference the same CR identifier.

5\. If a bug report exists, all related debug, fix, review, and test files must reference the same BUG identifier.



\## Standard Flow



Repository analysis:

\- /plans/repo\_index.md

\- /plans/architecture\_review.md



Feature work:

\- /changes/CR-XXX-feature-name.md

\- /plans/IMP-CR-XXX.md

\- /changes/CR-XXX-implementation-log.md

\- /reviews/CR-XXX-code-review.md

\- /tests/CR-XXX-test-report.md



Bug work:

\- /changes/BUG-XXX-title.md

\- /plans/BUG-XXX-debug-report.md

\- /changes/BUG-XXX-fix-log.md

\- /reviews/BUG-XXX-review.md

\- /tests/BUG-XXX-test-report.md

