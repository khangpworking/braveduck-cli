# Tasks

The owner answers the two open questions in design.md before work starts; the tasks
below follow the recommended answers.

## 1. Default rating model in the service

- [ ] 1.1 Read "not picked" as Clef unless the saved raters include Jev (internal/service decision settings); verify with unit tests for the three cases: saved setting, Jev among saved raters, nothing saved
- [ ] 1.2 Make automatic enabling pick Clef when a Clef token is saved and Jev when a Jev key is the only key; verify with tests for the four R-RAT05 scenarios and that `go test -race ./...` passes
- [ ] 1.3 Update `braveduck jev status` wording for the new default; verify with the command's test and by running it on a fresh data dir

## 2. Board

- [ ] 2.1 Preselect Clef in the default-rater dropdown and rating window when nothing was picked; update `web/src/locales/vi.json`; verify with `npm run typecheck && npm run lint && npm test`
- [ ] 2.2 Rebuild the embedded board (`npm run build`) and commit `internal/api/app`; verify CI's stale-build check passes

## 3. Docs

- [ ] 3.1 Update README and `docs/specs/jev-rating.md` so neither calls Jev the default; verify by searching both for "default" and reading each hit

## 4. Integration check

- [ ] 4.1 On a copy of a data dir that rates with Jev, start the new build and verify the badge's rater is still Jev; on a fresh data dir with a Clef token, verify the next scheduled run rates with `clef-flash`

## Workflow follow-up

- After the code merges: archive this change so `openspec/specs/rating/spec.md` carries the new R-RAT04 and R-RAT05.
