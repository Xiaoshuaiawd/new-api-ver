# Log Real Consumption Design

## Goal

Show administrators a `Real Consumption` total immediately after the existing
usage total in the common usage-log header. The total is calculated over the
same filters as the log table and is not returned to non-administrator clients.

## Accounting Rules

- Wallet-funded usage is valued from the final quota written to the consume
  log: `quota / QuotaPerUnit`. The final quota already contains the effective
  group multiplier, so multiplying by the group ratio again would understate
  the amount. This produces `230` for a `$1,000` upstream cost with a `0.23`
  group multiplier.
- Subscription-funded usage is valued as
  `subscription_consumed / subscription_total * subscription_price`.
  A `32`-yuan, `200`-dollar subscription therefore values a `15`-dollar
  request at `2.40` yuan.
- Monetary values are calculated with decimal arithmetic, rounded to fen, and
  persisted as integer cents. This avoids floating-point drift in aggregates.
- Each user subscription snapshots its plan price at creation. Consume logs
  snapshot their own calculated real-consumption amount. Plan price and group
  ratio changes therefore never rewrite historical revenue.

## Data Flow And Access Control

1. The subscription record receives an immutable price snapshot when it is
   created.
2. The existing consume-log path calculates `real_consumption_cents` and saves
   it alongside the log, including every existing provider/task log path that
   uses `RecordConsumeLog`.
3. The administrator log-stat query sums this column with the same consume-log
   filters used by the quota total and returns it as `real_consumption_cents`.
4. The self-service log-stat endpoint retains its existing response shape; it
   never returns this value.
5. The administrator UI renders `Real Consumption` directly after `Usage`,
   formatted as yuan and respecting the existing sensitive-data visibility
   control.

## Scope And Historical Data

The total is exact for logs created after this feature is deployed. Older
subscription logs lack an immutable purchase-price snapshot, so they are not
backfilled from mutable plan data. Existing database migrations must remain
compatible with SQLite, MySQL, and PostgreSQL.

## Verification

- Go tests cover wallet and subscription calculations, including the `15 / 200
  * 32 = 2.40` case and administrator-only response behavior.
- Frontend tests cover the administrator header placement and masking.
- Run targeted Go tests plus frontend tests, typecheck, lint, and build.
