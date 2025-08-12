# Minimal Query Optimization Modifications

## Solution 1: Quick Fix - Always Use Committed State (RECOMMENDED)
**File:** `baseapp/abci.go`
**Function:** `CreateQueryContextWithCheckHeader` (line ~1208)

### Change:
Replace lines 1240-1270 with:

```go
func (app *BaseApp) CreateQueryContextWithCheckHeader(height int64, prove, checkHeader bool) (sdk.Context, error) {
	if err := checkNegativeHeight(height); err != nil {
		return sdk.Context{}, err
	}

	// use custom query multi-store if provided
	qms := app.qms
	if qms == nil {
		qms = app.cms.(storetypes.MultiStore)
	}

	lastBlockHeight := qms.LatestVersion()
	if lastBlockHeight == 0 {
		return sdk.Context{}, errorsmod.Wrapf(sdkerrors.ErrInvalidHeight, "%s is not ready; please wait for first block", app.Name())
	}

	if height > lastBlockHeight {
		return sdk.Context{},
			errorsmod.Wrap(
				sdkerrors.ErrInvalidHeight,
				"cannot query with height in the future; please provide a valid height",
			)
	}

	if height == 1 && prove {
		return sdk.Context{},
			errorsmod.Wrap(
				sdkerrors.ErrInvalidRequest,
				"cannot query with proof when height <= 1; please provide a valid height",
			)
	}

	// SOLUTION 1: Skip state checking for latest queries - always use committed state
	var header *cmtproto.Header
	isLatest := height == 0
	
	if isLatest {
		// Force use of committed state only - skip checking finalizeBlockState
		height = lastBlockHeight
		if app.checkState != nil {
			h := app.checkState.Context().BlockHeader()
			h.Height = height // Update height to committed
			header = &h
		} else {
			// Create minimal header
			header = &cmtproto.Header{
				ChainID: app.chainID,
				Height:  height,
			}
		}
	} else {
		// Original logic for specific heights
		for _, state := range []*state{
			app.checkState,
			app.finalizeBlockState,
		} {
			if state != nil {
				h := state.Context().BlockHeader()
				if !checkHeader || h.Height == height {
					header = &h
					break
				}
			}
		}
	}

	if header == nil {
		return sdk.Context{},
			errorsmod.Wrapf(
				sdkerrors.ErrInvalidHeight,
				"context did not contain latest block height in either check state or finalize block state (%d)", lastBlockHeight,
			)
	}

	// Continue with rest of function unchanged...
	cacheMS, err := qms.CacheMultiStoreWithVersion(height)
	// ... rest remains the same
```

---

## Solution 2: Add Configuration Flag
**File:** `baseapp/baseapp.go`
**Add to BaseApp struct (line ~64):**

```go
type BaseApp struct {
	// ... existing fields ...
	
	// SOLUTION 2: Add query configuration
	fastQueries bool // Skip in-progress state for queries when true
	
	// ... rest of fields
}
```

**Add option function (at end of file):**
```go
// SetFastQueries enables fast query mode that always uses committed state
func SetFastQueries() func(*BaseApp) {
	return func(app *BaseApp) {
		app.fastQueries = true
	}
}
```

**Modify `CreateQueryContextWithCheckHeader`:**
```go
// Around line 1240, modify the isLatest handling:
isLatest := height == 0

// SOLUTION 2: Check fast query mode
if isLatest && app.fastQueries {
	// Force committed state
	height = lastBlockHeight
	if app.checkState != nil {
		h := app.checkState.Context().BlockHeader()
		h.Height = height
		header = &h
	} else {
		header = &cmtproto.Header{
			ChainID: app.chainID,
			Height:  height,
		}
	}
} else {
	// Original logic
	for _, state := range []*state{
		app.checkState,
		app.finalizeBlockState,
	} {
		// ... existing code
	}
}
```

---

## Solution 3: Async Query Store (More Complex)
**File:** `baseapp/baseapp.go`
**Add to BaseApp struct:**

```go
type BaseApp struct {
	// ... existing fields ...
	
	// SOLUTION 3: Dedicated query store updated async
	qmsCommitted      storetypes.MultiStore // Committed state for queries
	qmsCommittedMutex sync.RWMutex          // Protect qmsCommitted
}
```

**File:** `baseapp/abci.go`
**In `Commit` function (around line ~900), add after successful commit:**

```go
func (app *BaseApp) Commit() (*abci.ResponseCommit, error) {
	// ... existing commit logic ...
	
	// SOLUTION 3: Update dedicated query store (async)
	go func() {
		app.qmsCommittedMutex.Lock()
		defer app.qmsCommittedMutex.Unlock()
		
		// Update query store to latest committed
		if ms, err := app.cms.CacheMultiStoreWithVersion(app.cms.LatestVersion()); err == nil {
			app.qmsCommitted = ms
		}
	}()
	
	return res, nil
}
```

**Modify `CreateQueryContextWithCheckHeader`:**
```go
// Use dedicated query store if available
qms := app.qms
if qms == nil {
	// SOLUTION 3: Try dedicated committed store first
	app.qmsCommittedMutex.RLock()
	if app.qmsCommitted != nil {
		qms = app.qmsCommitted
	}
	app.qmsCommittedMutex.RUnlock()
	
	if qms == nil {
		qms = app.cms.(storetypes.MultiStore)
	}
}
```

---

## Solution 4: Simple Query Cache
**File:** `baseapp/baseapp.go`
**Add simple cache to BaseApp:**

```go
type BaseApp struct {
	// ... existing fields ...
	
	// SOLUTION 4: Simple query cache
	queryCache     map[string][]byte
	queryCacheMutex sync.RWMutex
}
```

**File:** `baseapp/abci.go`
**Modify `handleQueryGRPC` (line ~1152):**

```go
func (app *BaseApp) handleQueryGRPC(handler GRPCQueryHandler, req *abci.RequestQuery) *abci.ResponseQuery {
	// SOLUTION 4: Check cache for latest queries
	if req.Height == 0 && app.queryCache != nil {
		cacheKey := req.Path
		app.queryCacheMutex.RLock()
		if cached, found := app.queryCache[cacheKey]; found {
			app.queryCacheMutex.RUnlock()
			return &abci.ResponseQuery{
				Value:  cached,
				Height: app.cms.LatestVersion(),
			}
		}
		app.queryCacheMutex.RUnlock()
	}
	
	ctx, err := app.CreateQueryContext(req.Height, req.Prove)
	if err != nil {
		return sdkerrors.QueryResult(err, app.trace)
	}

	resp, err := handler(ctx, req)
	if err != nil {
		resp = sdkerrors.QueryResult(gRPCErrorToSDKError(err), app.trace)
		resp.Height = req.Height
		return resp
	}
	
	// SOLUTION 4: Cache successful latest queries
	if req.Height == 0 && app.queryCache != nil && err == nil {
		app.queryCacheMutex.Lock()
		app.queryCache[req.Path] = resp.Value
		app.queryCacheMutex.Unlock()
	}

	return resp
}
```

**In `Commit`, clear cache:**
```go
// SOLUTION 4: Clear query cache on commit
if app.queryCache != nil {
	app.queryCacheMutex.Lock()
	app.queryCache = make(map[string][]byte)
	app.queryCacheMutex.Unlock()
}
```

---

## Simplest Implementation (Just Solution 1)

If you want the absolute minimal change, just modify `baseapp/abci.go` line ~1240-1270:

```diff
- 	var header *cmtproto.Header
- 	isLatest := height == 0
- 	for _, state := range []*state{
- 		app.checkState,
- 		app.finalizeBlockState,
- 	} {
- 		if state != nil {
- 			// branch the commit multi-store for safety
- 			h := state.Context().BlockHeader()
- 			if isLatest {
- 				lastBlockHeight = qms.LatestVersion()
- 			}
- 			if !checkHeader || !isLatest || isLatest && h.Height == lastBlockHeight {
- 				header = &h
- 				break
- 			}
- 		}
- 	}
+ 	var header *cmtproto.Header
+ 	isLatest := height == 0
+ 	
+ 	// SOLUTION 1: For latest queries, always use committed state
+ 	if isLatest {
+ 		height = lastBlockHeight
+ 		if app.checkState != nil {
+ 			h := app.checkState.Context().BlockHeader()
+ 			h.Height = height
+ 			header = &h
+ 		}
+ 	} else {
+ 		// Original logic for specific heights
+ 		for _, state := range []*state{
+ 			app.checkState,
+ 			app.finalizeBlockState,
+ 		} {
+ 			if state != nil {
+ 				h := state.Context().BlockHeader()
+ 				if !checkHeader || h.Height == height {
+ 					header = &h
+ 					break
+ 				}
+ 			}
+ 		}
+ 	}
```

This single change will make queries always return the last committed state when requesting "latest" (height=0), completely avoiding any blocking from EndBlockers.