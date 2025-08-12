package baseapp

import (
	"context"
	
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	
	errorsmod "cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// ============================================================================
// OPTIMIZED CreateQueryContext - Integrates all solutions
// ============================================================================

// CreateQueryContextOptimized is the main entry point that routes to different solutions
// based on configuration. This would replace the existing CreateQueryContext method.
func (app *BaseApp) CreateQueryContextOptimized(height int64, prove bool) (sdk.Context, error) {
	// Check which solution is enabled and route accordingly
	
	// SOLUTION 3: Highest priority - Use async query store if available
	// if app.asyncQueryStore != nil && app.asyncQueryStore.enabled.Load() {
	//     return app.asyncQueryStore.GetQueryContext(height, prove)
	// }
	
	// SOLUTION 2: Check query mode configuration
	// if app.queryConfig.Mode != QueryModeDefault {
	//     switch app.queryConfig.Mode {
	//     case QueryModeFast:
	//         return app.createQueryContextFast(height, prove)
	//     case QueryModeEventual:
	//         return app.createQueryContextEventual(height, prove)
	//     }
	// }
	
	// SOLUTION 1: Default optimization - always use committed state for latest
	if height == 0 {
		return app.createQueryContextCommittedOnly(height, prove)
	}
	
	// Fall back to original implementation for specific heights
	return app.CreateQueryContextWithCheckHeader(height, prove, true)
}

// ============================================================================
// SOLUTION 1: Always Use Committed State
// ============================================================================

// createQueryContextCommittedOnly implements Solution 1
// This bypasses any in-progress state and always returns the last committed state
func (app *BaseApp) createQueryContextCommittedOnly(height int64, prove bool) (sdk.Context, error) {
	if err := checkNegativeHeight(height); err != nil {
		return sdk.Context{}, err
	}
	
	// Use custom query multi-store if provided
	qms := app.qms
	if qms == nil {
		qms = app.cms.(storetypes.MultiStore)
	}
	
	lastBlockHeight := qms.LatestVersion()
	if lastBlockHeight == 0 {
		return sdk.Context{}, errorsmod.Wrapf(
			sdkerrors.ErrInvalidHeight, 
			"%s is not ready; please wait for first block", 
			app.Name(),
		)
	}
	
	// SOLUTION 1 CORE: For latest queries, force use of committed height
	if height == 0 {
		height = lastBlockHeight
	} else if height > lastBlockHeight {
		return sdk.Context{}, errorsmod.Wrap(
			sdkerrors.ErrInvalidHeight,
			"cannot query with height in the future; please provide a valid height",
		)
	}
	
	// Skip proof validation for height 1
	if height == 1 && prove {
		return sdk.Context{}, errorsmod.Wrap(
			sdkerrors.ErrInvalidRequest,
			"cannot query with proof when height <= 1; please provide a valid height",
		)
	}
	
	// Load the committed state at the specified height
	cacheMS, err := qms.CacheMultiStoreWithVersion(height)
	if err != nil {
		return sdk.Context{}, errorsmod.Wrapf(
			sdkerrors.ErrNotFound,
			"failed to load state at height %d; %s (latest height: %d)", 
			height, err, lastBlockHeight,
		)
	}
	
	// Create a minimal header for the committed state
	// NOTE: This header won't have all fields populated since we're bypassing state
	header := cmtproto.Header{
		ChainID: app.chainID,
		Height:  height,
		// TODO: Optionally retrieve more header info from commit info:
		// commitInfo, err := app.cms.GetCommitInfo(height)
		// if err == nil && commitInfo != nil {
		//     header.Time = commitInfo.Timestamp
		//     header.AppHash = commitInfo.Hash
		// }
	}
	
	// Create and return the query context
	ctx := sdk.NewContext(cacheMS, header, true, app.logger).
		WithMinGasPrices(app.minGasPrices).
		WithGasMeter(storetypes.NewGasMeter(app.queryGasLimit)).
		WithBlockHeight(height)
	
	if app.trace {
		ctx = ctx.WithTracer(app.tracer).WithTraceSpanContext(app.traceSpanContext)
	}
	
	return ctx, nil
}

// ============================================================================
// SOLUTION 2: Fast Mode Implementation
// ============================================================================

// createQueryContextFast implements Solution 2's fast mode
func (app *BaseApp) createQueryContextFast(height int64, prove bool) (sdk.Context, error) {
	// SOLUTION 2: Fast mode always uses committed state
	// This is essentially the same as Solution 1 but controlled by configuration
	
	if height == 0 {
		// Force use of latest committed height
		qms := app.qms
		if qms == nil {
			qms = app.cms.(storetypes.MultiStore) 
		}
		height = qms.LatestVersion()
	}
	
	// Reuse Solution 1's implementation
	return app.createQueryContextCommittedOnly(height, prove)
}

// ============================================================================
// SOLUTION 3: Eventual Consistency Mode
// ============================================================================

// createQueryContextEventual implements Solution 2's eventual consistency mode
// This would use the async query store from Solution 3
func (app *BaseApp) createQueryContextEventual(height int64, prove bool) (sdk.Context, error) {
	// SOLUTION 3 Integration: Use async query store if available
	// if app.asyncQueryStore != nil {
	//     return app.asyncQueryStore.GetQueryContext(height, prove)
	// }
	
	// Fallback to fast mode if async store not available
	return app.createQueryContextFast(height, prove)
}

// ============================================================================
// OPTIMIZED QUERY HANDLER
// ============================================================================

// handleQueryGRPCOptimized replaces handleQueryGRPC with optimization support
func (app *BaseApp) handleQueryGRPCOptimized(handler GRPCQueryHandler, req *abci.RequestQuery) *abci.ResponseQuery {
	// SOLUTION 4: Check cache first if enabled
	// if app.queryCache != nil {
	//     cacheKey := fmt.Sprintf("%s:%d:%v", req.Path, req.Height, req.Prove)
	//     if data, height, found := app.queryCache.Get(cacheKey); found {
	//         return &abci.ResponseQuery{
	//             Value:  data,
	//             Height: height,
	//             Info:   "cached", // Indicate this was cached
	//         }
	//     }
	// }
	
	// Create appropriate context based on configuration
	ctx, err := app.CreateQueryContextOptimized(req.Height, req.Prove)
	if err != nil {
		return sdkerrors.QueryResult(err, app.trace)
	}
	
	// Execute the query
	resp, err := handler(ctx, req)
	if err != nil {
		resp = sdkerrors.QueryResult(gRPCErrorToSDKError(err), app.trace)
		resp.Height = req.Height
		return resp
	}
	
	// SOLUTION 4: Cache successful results
	// if app.queryCache != nil && err == nil {
	//     cacheKey := fmt.Sprintf("%s:%d:%v", req.Path, req.Height, req.Prove)
	//     app.queryCache.Set(cacheKey, resp.Value, resp.Height)
	// }
	
	return resp
}

// ============================================================================
// COMMIT INTEGRATION POINTS
// ============================================================================

// CommitOptimized shows where to integrate query optimizations with Commit
func (app *BaseApp) CommitOptimized() (*abci.ResponseCommit, error) {
	// ... existing commit logic ...
	
	// After successful commit:
	
	// SOLUTION 3: Update async query store
	// This runs asynchronously to avoid blocking consensus
	// if app.asyncQueryStore != nil {
	//     go func() {
	//         app.asyncQueryStore.SignalUpdate()
	//     }()
	// }
	
	// SOLUTION 4: Invalidate or clear cache
	// Option 1: Clear entire cache (simple but less efficient)
	// if app.queryCache != nil {
	//     app.queryCache.Clear()
	// }
	
	// Option 2: Smart invalidation (TODO: implement)
	// if app.queryCache != nil {
	//     app.queryCache.InvalidateHeight(app.LastBlockHeight())
	// }
	
	// ... return commit response ...
	return nil, nil // Placeholder
}

// ============================================================================
// QUERY METHOD OVERRIDE
// ============================================================================

// QueryOptimized would replace the existing Query method in abci.go
func (app *BaseApp) QueryOptimized(ctx context.Context, req *abci.RequestQuery) (resp *abci.ResponseQuery, err error) {
	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			err = sdkerrors.ErrPanic
		}
		resp = sdkerrors.QueryResult(err, app.trace)
	}()
	
	// Reject broadcast tx requests
	if req.Path == QueryPathBroadcastTx {
		return sdkerrors.QueryResult(
			errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "can't route a broadcast tx message"), 
			app.trace,
		), nil
	}
	
	// Route gRPC queries through optimized handler
	if grpcHandler := app.grpcQueryRouter.Route(req.Path); grpcHandler != nil {
		// Use optimized handler instead of original
		return app.handleQueryGRPCOptimized(grpcHandler, req), nil
	}
	
	// Handle other query paths (app, store, p2p) as before
	path := SplitABCIQueryPath(req.Path)
	if len(path) == 0 {
		return sdkerrors.QueryResult(
			errorsmod.Wrap(sdkerrors.ErrUnknownRequest, "no query path provided"), 
			app.trace,
		), nil
	}
	
	switch path[0] {
	case QueryPathApp:
		resp = handleQueryApp(app, path, req)
	case QueryPathStore:
		resp = handleQueryStore(app, path, *req)
	case QueryPathP2P:
		resp = handleQueryP2P(app, path)
	default:
		resp = sdkerrors.QueryResult(
			errorsmod.Wrap(sdkerrors.ErrUnknownRequest, "unknown query path"), 
			app.trace,
		)
	}
	
	return resp, nil
}

// ============================================================================
// CONFIGURATION HELPERS
// ============================================================================

// ConfigureQueryOptimizations sets up query optimizations based on config
func (app *BaseApp) ConfigureQueryOptimizations(config QueryConfig) {
	// TODO: Store config in app
	// app.queryConfig = config
	
	// SOLUTION 3: Initialize async query store if requested
	// if config.EnableAsyncQueryStore {
	//     app.asyncQueryStore = NewAsyncQueryStore(app, app.logger, config.QueryStoreLagBlocks)
	//     app.asyncQueryStore.Start()
	// }
	
	// SOLUTION 4: Initialize query cache if requested
	// if config.EnableQueryCache {
	//     app.queryCache = NewQueryCache(config.QueryCacheTTL)
	// }
}

// ============================================================================
// NOTES ON INTEGRATION
// ============================================================================

/*
To integrate these optimizations into your Cosmos SDK application:

1. MINIMAL CHANGE (Solution 1 only):
   - Replace CreateQueryContext with createQueryContextCommittedOnly
   - No configuration needed, always uses committed state for latest queries

2. WITH CONFIGURATION (Solution 2):
   - Add QueryConfig to BaseApp struct
   - Add ConfigureQueryOptimizations to app initialization
   - Set query mode via config or CLI flags

3. WITH ASYNC STORE (Solution 3):
   - Add AsyncQueryStore to BaseApp struct
   - Call UpdateAsyncQueryStore() from Commit()
   - Configure lag blocks based on your requirements

4. WITH CACHING (Solution 4):
   - Add QueryCache to BaseApp struct
   - Configure TTL based on your block time
   - Consider cache size limits and eviction policies

RECOMMENDED APPROACH:
- Start with Solution 1 (immediate fix, no config needed)
- Add Solution 2 for configurability
- Consider Solution 3 for read-heavy workloads
- Add Solution 4 if query patterns are repetitive

TESTING:
- Test with heavy EndBlockers to verify queries don't block
- Monitor query latencies before and after optimization
- Verify data consistency with different configurations
*/