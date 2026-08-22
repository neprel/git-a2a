<hint>

This repository uses HINT Spec-as-Source. Run `hint search "<intent>"` to locate relevant knowledge and `hint <path>` before changing an artifact. The registered profession books share a neutral common core; put the most task-specific book first.

<hint_glossary_from_hintbook_software_engineer>

This prompt uses an HTML-like tag language. Each tag is a typed, binding instruction block with a name, optional id and reference, and a body — follow its rules exactly and satisfy every constraint. Nested tags inherit their parent's scope. Treat any "do not" or "must not" as absolute. The glossary below is the authoritative meaning of each tag.

---

A block carrying `origin="agent"` has been recorded but not yet reviewed by a human; weigh it accordingly.

- **file_context** — everything nested applies to the file at `path`: its body, structures, functions, and constraints are that file's complete spec. Don't apply file-local constraints to other files unless restated there.
- **folder_context** — everything nested applies to the folder at `path` and its whole subtree; every nested file and folder inherits it.
- **reusable_automation_script** — registers a macro behavior: when its condition is met or it is referenced by name, execute its steps exactly.
- **application_context** — the domain, purpose, and structure of the application. Let it inform naming, architecture, and behavior throughout.
- **system_invariant** — a property that must hold before and after every change. Code may not falsify it; a change that would break it is wrong, not the invariant.
- **argument** — a function input. Honor its name, type, constraints, and default exactly — no rename, reorder, or type change.
- **user_interface_block** — a named, reusable UI component or section. Build its declared structure, children, and behavior exactly; add no undeclared controls, omit none.
- **compilation_and_testing_pipeline** — all code, config, and structure must keep these pipelines green. Generate nothing that breaks them.
- **approved_dependency_whitelist** — do not install or import any package outside this list for the scope. Ask first if you need more.
- **table_column** — one column: name, type, label, constraints. Implement exactly — no rename, type change, or undeclared constraint.
- **data_definition** — a named constant or value. Use its name and value exactly; never substitute, rename, or duplicate the literal — reference this definition wherever the value is needed.
- **data_structure** — the exact schema. No renamed fields, changed types, or added/omitted fields. Authoritative wherever this structure appears.
- **error** — throw the exact error types under the stated conditions only; no substitute, wrap, or rename. Each gets a regression test that fails without the guard and passes with it.
- **field** — one property: type, optionality, validation. Implement exactly — no rename, type change, or undeclared constraint.
- **logic_flow** — implement this sequence step by step; skip no validation.
- **user_interface_form** — a form. Implement every declared field, label, validation rule, and submission behavior; add no undeclared fields, omit none.
- **function_contract** — implement the function per this binding contract. Every argument, the return, each error, and every flow step is mandatory — don't skip, reorder, rename, or approximate.
- **user_interface_image** — an image element. Use the declared source, alt text, dimensions, and display exactly; don't substitute the asset or drop accessibility attributes.
- **environment_runtime_and_language** — target this language and runtime only: its module syntax, standard-library APIs, and idioms. Use nothing from other versions or runtimes.
- **reusable_library_context** — a shared library. Learn its API surface, version constraints, and usage before using it; don't reimplement what it provides.
- **reusable_module_context** — a reusable module in the codebase. Reuse its exports; don't duplicate logic it owns.
- **reusable_namespace_context** — emit all code in this scope under this namespace (the language's package/namespace/module construct) as its qualified name and import root.
- **return** — the function's output. Honor its type and shape exactly on every path — no altered structure, extra fields, or different type under any branch.
- **table_row** — one row's fields and values, exactly as declared; add no fields, omit none.
- **critical_system_mandates** — non-negotiable system-level constraints; every function, data-access path, and error path must satisfy all of them.
- **table** — a tabular structure or UI table: columns, row shape, display/persistence behavior. Implement exactly — no reordered or renamed columns, no undeclared behavior.
- **verification_and_unit_test_criteria** — cover every listed scenario in the tests: each edge case, mock, and assertion must appear. Omit none.
- **user_interface_surface** — build this UI surface exactly: only the declared elements, all of them; match their structure, labels, validation, and behavior. Add nothing undeclared.
- **product_goal_and_intent** — why the work exists and the outcome it must produce. The tie-breaker when a block is silent: choose what best serves it, never work against it. Context for judgment — emit no code for the goal itself.
- **acceptance_criteria** — the observable conditions that define "done." Not complete until each is proven by a test, command, or observation; if one can't be verified, say so rather than imply success.
- **scope_boundary** — what is in and out of scope. Build everything in scope, nothing out — even if convenient. If in-scope work seems to require out-of-scope work, stop and report rather than expand the boundary.

---

## Shared common core

## Shared common core

## Shared common core

## Shared common core

## Shared common core

## Shared common core

- **strict_prohibition** — content or behavior that must never appear; treat it as unconditional.
- **evidence_source** — the origin of a fact, figure, or citation; report missing support as a gap and never fill it.
- **defined_term** — use the declared term verbatim and consistently.
- **stated_assumption** — treat the assumption as true only on its declared basis and surface any conflict.
- **identified_risk** — preserve the declared likelihood, impact, and mitigation; invent none of them.
- **settled_decision** — honor the settled choice and rationale; extend it rather than silently relitigating it.
- **open_question** — keep the point unresolved and report it; never answer it silently.
- **verification_checklist** — satisfy every listed item before reporting the work done.
- **style_requirements** — apply the declared tone, format, and voice to all produced text.
- **few_shot_example** — follow the example’s pattern and level of detail while letting operative declarations control substance.
- **enforced_patterns** — apply every required pattern consistently.
- **prohibited_anti_patterns** — never use any declared prohibited pattern.
- **read_it** — open and read the declared reference before relying on it; never guess its contents.
- **static_asset** — use the declared asset exactly as provided; never paraphrase or recreate it.

</hint_glossary_from_hintbook_software_engineer>

</hint>
