# Log Real Consumption Design

## Goal

Show administrators a `Real Consumption` total immediately after the existing
usage total in the common usage-log header. The total is calculated over the
same filters as the log table and is not returned to non-administrator clients.

## Accounting Rules

- Wallet-funded usage is valued as `actual quota consumption * group_ratio`.
  The log stores quota in internal units, so the calculation first divides by
  the group ratio and `QuotaPerUnit`, then applies the group ratio explicitly.
  This produces `230` for a `1,000`-unit actual quota consumption with a `0.23`
  group multiplier.
- Subscription-funded usage is valued as
  `subscription_consumed / subscription_total * subscription_price`.
  A `32`-yuan, `200`-dollar subscription therefore values a `15`-dollar
  request at `2.40` yuan.
- Monetary values are calculated with decimal arithmetic from persisted quota
  and billing metadata at query time. Values are summed before the UI rounds
  them for display, so small requests do not each become `0.01`.
- Each user subscription snapshots its plan price at creation. The group ratio
  and subscription price written to each log keep historical calculations
  stable when settings change later.

## Data Flow And Access Control

1. The subscription record receives an immutable price snapshot when it is
   created.
2. Each consume log retains its quota, group ratio, subscription amount and
   immutable subscription-price snapshot in billing metadata.
3. The administrator log-stat query calculates and sums `real_consumption`
   with the same consume-log filters used by the quota total.
4. The self-service log-stat endpoint retains its existing response shape; it
   never returns this value.
5. The administrator UI renders `Real Consumption` directly after `Actual Quota Consumption`,
   formatted as yuan and respecting the existing sensitive-data visibility
   control.

## Scope And Historical Data

The total is exact for wallet logs that retain their group ratio and for
subscription logs that retain a price snapshot. Older subscription logs that
lack an immutable purchase-price snapshot contribute zero rather than being
recomputed from mutable plan data. Existing database migrations must remain
compatible with SQLite, MySQL, and PostgreSQL.

## Verification

- Go tests cover wallet and subscription calculations, including the `15 / 200
  * 32 = 2.40` case and administrator-only response behavior.
- Frontend tests cover the administrator header placement and masking.
- Run targeted Go tests plus frontend tests, typecheck, lint, and build.
