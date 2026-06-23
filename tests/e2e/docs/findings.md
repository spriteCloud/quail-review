# Bug discovery ledger

Persists fuzz/negative/journey failures across runs. Each row is one
finding, deduped by (spec, test). Update with:

```bash
quail ledger update --report playwright-report.json
```

Severity follows the @priority mapping: critical journeys → high,
standard → medium, nice-to-have → low.

| Spec | Test | Symptom | First seen | Last seen | Severity | Status |
|---|---|---|---|---|---|---|
