# Fix: Auto-NLBs-V2 Service Prewarming Bug When ReserveNlbNum=0

## Summary

Fix two bugs in the `prewarmServices` logic of the Auto-NLBs-V2 network plugin that cause services to be bound to the wrong NLB instance when `ReserveNlbNum` is set to 0.

## Problem Description

When `ReserveNlbNum=0` and the number of pods exceeds the capacity of a single NLB (podsPerNLB), newly created services may be incorrectly associated with the previous NLB instead of the newly created one. This results in pods being routed through the wrong NLB instance.

### Root Cause

Two independent bugs contribute to this issue:

**Bug 1: Stale NLB list in `ensurePrewarming`**

In `ensurePrewarming()`, the NLB list is queried (line ~644) before new NLBs are created (lines ~662-677). The stale list is then passed to `prewarmServices()` (line ~680), causing newly created NLBs to be excluded from service prewarming.

**Bug 2: Array index used instead of real Pool Index in `prewarmServices`**

In `prewarmServices()`, the loop variable `nlbIdx` (array index from List result) is used to calculate `globalServiceIndex` (line ~743). Since Kubernetes List API does not guarantee ordering, when NLBs are returned in a different order than their pool index, services are created with incorrect pod name mappings and bound to the wrong NLB.

### Reproduction Scenario

1. Configure `ReserveNlbNum=0`, `podsPerNLB=250`
2. Scale GSS from 250 to 251 pods
3. Pod-250 should map to NLB-1, but its service gets bound to NLB-0

## Solution

### Fix 1: Re-query NLB list after creation

After creating new NLBs in `ensurePrewarming`, re-query the NLB list before passing it to `prewarmServices` to ensure all NLBs (including newly created ones) are included.

### Fix 2: Use `NLBPoolIndexLabel` instead of array index

Replace the array index `nlbIdx` with the real pool index from the NLB's label (`game.kruise.io/nlb-pool-index`) when calculating `globalServiceIndex`. This makes the calculation independent of List return order.

## Affected Files

- `cloudprovider/alibabacloud/auto_nlbs_v2.go`

## Backward Compatibility

- No API changes
- No configuration changes
- Existing NLB instances with correct `NLBPoolIndexLabel` labels work without migration
- NLBs without the label are safely skipped with a warning log

## Why ReserveNlbNum>=1 masks this bug

When `ReserveNlbNum>=1`, new NLBs and their services are always prewarmed ahead of time (before any pod needs them). The stale list and wrong index bugs still exist but never manifest because services are already correctly created before the boundary pod appears.
