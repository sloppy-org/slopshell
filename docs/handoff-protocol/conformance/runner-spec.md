# Conformance Runner Notes

A runner should validate:

1. Envelope schema validation for positive examples.
2. Kind-specific schema validation for positive examples.
3. Negative examples must fail schema validation.
4. Producer behavior checks:
   - `consume` decrements/advances counters.
   - expired/revoked handoffs are rejected.
