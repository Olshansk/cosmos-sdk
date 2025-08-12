package baseapp

import (
	"sync"
	"sync/atomic"
	"time"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"cosmossdk.io/log"
	errorsmod "cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// ============================================================================
// SOLUTION 2: Query Mode Configuration
// ============================================================================

// QueryMode defines how queries should handle in-progress state
type QueryMode int

const (
	// QueryModeDefault - Original behavior, may wait for in-progress state
	QueryModeDefault QueryMode = iota
	// QueryModeFast - Always use last committed state, never wait
	QueryModeFast
	// QueryModeEventual - Use eventually consistent read-only store
	QueryModeEventual
)

// QueryConfig holds configuration for query optimization
type QueryConfig struct {
	// Mode determines query behavior
	Mode QueryMode
	
	// EnableAsyncQueryStore enables Solution 3's async query store
	EnableAsyncQueryStore bool
	
	// QueryStoreLagBlocks - how many blocks the query store can lag behind (Solution 3)
	QueryStoreLagBlocks int
	
	// EnableQueryCache enables result caching (Solution 4)
	EnableQueryCache bool
	
	// QueryCacheTTL - how long to cache query results
	QueryCacheTTL time.Duration
}

// ============================================================================
// SOLUTION 3: Dedicated Query Store Components
// ============================================================================

// AsyncQueryStore manages a dedicated read-only store updated asynchronously
type AsyncQueryStore struct {
	mu sync.RWMutex
	
	// The dedicated query multistore
	qms storetypes.MultiStore
	
	// Channel to signal updates
	updateCh chan struct{}
	
	// Current committed height of query store
	committedHeight int64
	
	// Logger
	logger log.Logger
	
	// Reference to parent app for getting committed state
	app *BaseApp
	
	// Controls if async updates are enabled
	enabled atomic.Bool
	
	// Lag configuration - how many blocks behind to stay
	lagBlocks int
}

// NewAsyncQueryStore creates a new async query store
func NewAsyncQueryStore(app *BaseApp, logger log.Logger, lagBlocks int) *AsyncQueryStore {
	return &AsyncQueryStore{
		updateCh:  make(chan struct{}, 1),
		logger:    logger,
		app:       app,
		lagBlocks: lagBlocks,
	}
}

// Start begins the async update loop
func (aqs *AsyncQueryStore) Start() {
	aqs.enabled.Store(true)
	go aqs.updateLoop()
}

// Stop stops the async update loop
func (aqs *AsyncQueryStore) Stop() {
	aqs.enabled.Store(false)
	close(aqs.updateCh)
}

// updateLoop runs in a goroutine to update the query store
func (aqs *AsyncQueryStore) updateLoop() {
	for {
		select {
		case <-aqs.updateCh:
			if !aqs.enabled.Load() {
				return
			}
			aqs.updateQueryStore()
		}
	}
}

// SignalUpdate signals that a new update is available
func (aqs *AsyncQueryStore) SignalUpdate() {
	select {
	case aqs.updateCh <- struct{}{}:
		// Signaled successfully
	default:
		// Channel full, update already pending
	}
}

// updateQueryStore updates the dedicated query store
func (aqs *AsyncQueryStore) updateQueryStore() {
	// TODO: Implement actual store synchronization
	// This would involve:
	// 1. Getting the latest committed version from app.cms
	// 2. Creating a new cache multistore at that version
	// 3. Swapping it atomically
	
	aqs.mu.Lock()
	defer aqs.mu.Unlock()
	
	targetHeight := aqs.app.cms.LatestVersion() - int64(aqs.lagBlocks)
	if targetHeight <= aqs.committedHeight {
		return // Already up to date
	}
	
	// TODO: Create new multistore at targetHeight
	// newQMS, err := aqs.app.cms.CacheMultiStoreWithVersion(targetHeight)
	// if err != nil {
	//     aqs.logger.Error("failed to update query store", "err", err, "height", targetHeight)
	//     return
	// }
	// aqs.qms = newQMS
	aqs.committedHeight = targetHeight
}

// GetQueryContext returns a context for queries using the async store
func (aqs *AsyncQueryStore) GetQueryContext(height int64, prove bool) (sdk.Context, error) {
	aqs.mu.RLock()
	defer aqs.mu.RUnlock()
	
	if aqs.qms == nil {
		return sdk.Context{}, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "async query store not initialized")
	}
	
	// TODO: Implement context creation from async query store
	// This would be similar to CreateQueryContext but using aqs.qms
	
	return sdk.Context{}, nil
}

// ============================================================================
// SOLUTION 4: Query Result Cache
// ============================================================================

// QueryCache caches query results to avoid repeated processing
type QueryCache struct {
	mu    sync.RWMutex
	cache map[string]*CachedResult
	ttl   time.Duration
}

// CachedResult holds a cached query result
type CachedResult struct {
	Data      []byte
	Height    int64
	Timestamp time.Time
}

// NewQueryCache creates a new query cache
func NewQueryCache(ttl time.Duration) *QueryCache {
	qc := &QueryCache{
		cache: make(map[string]*CachedResult),
		ttl:   ttl,
	}
	
	// TODO: Implement cache eviction goroutine
	// go qc.evictionLoop()
	
	return qc
}

// Get retrieves a cached result if valid
func (qc *QueryCache) Get(key string) ([]byte, int64, bool) {
	qc.mu.RLock()
	defer qc.mu.RUnlock()
	
	result, exists := qc.cache[key]
	if !exists {
		return nil, 0, false
	}
	
	// Check if result is still valid
	if time.Since(result.Timestamp) > qc.ttl {
		return nil, 0, false
	}
	
	return result.Data, result.Height, true
}

// Set stores a query result in cache
func (qc *QueryCache) Set(key string, data []byte, height int64) {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	
	qc.cache[key] = &CachedResult{
		Data:      data,
		Height:    height,
		Timestamp: time.Now(),
	}
}

// Clear removes all cached results
func (qc *QueryCache) Clear() {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	
	qc.cache = make(map[string]*CachedResult)
}

// ============================================================================
// EXTENDED BASEAPP FOR QUERY OPTIMIZATIONS
// ============================================================================

// Add these fields to BaseApp struct (in baseapp.go):
// queryConfig      QueryConfig        // Configuration for query optimizations
// asyncQueryStore  *AsyncQueryStore   // Solution 3: Dedicated async query store  
// queryCache       *QueryCache        // Solution 4: Query result cache

// ============================================================================
// SOLUTION 1: Modified CreateQueryContext - Always Use Committed State
// ============================================================================

// CreateQueryContextSolution1 - Always returns last committed state for latest queries
func (app *BaseApp) CreateQueryContextSolution1(height int64, prove bool) (sdk.Context, error) {
	if err := checkNegativeHeight(height); err != nil {
		return sdk.Context{}, err
	}
	
	qms := app.qms
	if qms == nil {
		qms = app.cms.(storetypes.MultiStore)
	}
	
	lastBlockHeight := qms.LatestVersion()
	if lastBlockHeight == 0 {
		return sdk.Context{}, errorsmod.Wrapf(sdkerrors.ErrInvalidHeight, "%s is not ready; please wait for first block", app.Name())
	}
	
	// SOLUTION 1: For latest queries (height == 0), always use committed state
	// This completely bypasses any in-progress state checking
	if height == 0 {
		height = lastBlockHeight
		
		// Create a minimal header for the committed state
		header := cmtproto.Header{
			ChainID: app.chainID,
			Height:  height,
			// TODO: Retrieve actual header info from commit info if needed
			// commitInfo := app.cms.GetCommitInfo(height)
		}
		
		cacheMS, err := qms.CacheMultiStoreWithVersion(height)
		if err != nil {
			return sdk.Context{}, errorsmod.Wrapf(
				sdkerrors.ErrNotFound,
				"failed to load state at height %d; %s (latest height: %d)", height, err, lastBlockHeight,
			)
		}
		
		// Return context with committed state only
		return sdk.NewContext(cacheMS, header, true, app.logger).
			WithMinGasPrices(app.minGasPrices).
			WithGasMeter(storetypes.NewGasMeter(app.queryGasLimit)).
			WithBlockHeight(height), nil
	}
	
	// For specific heights, use existing logic
	if height > lastBlockHeight {
		return sdk.Context{}, errorsmod.Wrap(
			sdkerrors.ErrInvalidHeight,
			"cannot query with height in the future; please provide a valid height",
		)
	}
	
	// TODO: Handle specific height queries
	// ... rest of original logic for specific heights
	
	return sdk.Context{}, nil
}

// ============================================================================
// SOLUTION 2: Query Mode Based Context Creation
// ============================================================================

// CreateQueryContextSolution2 - Uses query mode configuration
func (app *BaseApp) CreateQueryContextSolution2(height int64, prove bool) (sdk.Context, error) {
	// SOLUTION 2: Check query mode configuration
	var queryConfig QueryConfig // TODO: This should be app.queryConfig
	
	switch queryConfig.Mode {
	case QueryModeFast:
		// Fast mode: always use committed state
		if height == 0 {
			qms := app.qms
			if qms == nil {
				qms = app.cms.(storetypes.MultiStore)
			}
			height = qms.LatestVersion()
			
			// Skip all state checking, go straight to committed
			// TODO: Implement fast path similar to Solution 1
			return app.CreateQueryContextSolution1(height, prove)
		}
		
	case QueryModeEventual:
		// Use async query store if available
		// TODO: Check if app.asyncQueryStore is available
		// if app.asyncQueryStore != nil {
		//     return app.asyncQueryStore.GetQueryContext(height, prove)
		// }
		
	case QueryModeDefault:
		// Fall through to original behavior
	}
	
	// Original behavior for default mode or specific heights
	return app.CreateQueryContextWithCheckHeader(height, prove, true)
}

// ============================================================================
// SOLUTION 3: Async Query Store Integration
// ============================================================================

// CreateQueryContextSolution3 - Uses dedicated async query store
func (app *BaseApp) CreateQueryContextSolution3(height int64, prove bool) (sdk.Context, error) {
	// SOLUTION 3: Use dedicated async query store if enabled
	// TODO: Check if app.asyncQueryStore is initialized
	// if app.asyncQueryStore != nil && app.asyncQueryStore.enabled.Load() {
	//     return app.asyncQueryStore.GetQueryContext(height, prove)
	// }
	
	// Fallback to solution 1 behavior
	return app.CreateQueryContextSolution1(height, prove)
}

// UpdateAsyncQueryStore should be called after Commit
func (app *BaseApp) UpdateAsyncQueryStore() {
	// TODO: This should be called from Commit() method
	// if app.asyncQueryStore != nil {
	//     app.asyncQueryStore.SignalUpdate()
	// }
}

// ============================================================================
// SOLUTION 4: Cached Query Handler
// ============================================================================

// handleQueryGRPCWithCache wraps query handling with caching
func (app *BaseApp) handleQueryGRPCWithCache(handler GRPCQueryHandler, req *abci.RequestQuery) *abci.ResponseQuery {
	// SOLUTION 4: Check cache first
	// TODO: Initialize app.queryCache in BaseApp
	// if app.queryCache != nil {
	//     cacheKey := fmt.Sprintf("%s:%d:%v", req.Path, req.Height, req.Prove)
	//     if data, height, found := app.queryCache.Get(cacheKey); found {
	//         return &abci.ResponseQuery{
	//             Value:  data,
	//             Height: height,
	//         }
	//     }
	// }
	
	// Execute query
	ctx, err := app.CreateQueryContextSolution1(req.Height, req.Prove)
	if err != nil {
		return sdkerrors.QueryResult(err, app.trace)
	}
	
	resp, err := handler(ctx, req)
	if err != nil {
		resp = sdkerrors.QueryResult(gRPCErrorToSDKError(err), app.trace)
		resp.Height = req.Height
		return resp
	}
	
	// Cache successful result
	// TODO: Store in cache
	// if app.queryCache != nil && err == nil {
	//     cacheKey := fmt.Sprintf("%s:%d:%v", req.Path, req.Height, req.Prove)
	//     app.queryCache.Set(cacheKey, resp.Value, resp.Height)
	// }
	
	return resp
}

// ============================================================================
// HELPER: Options for BaseApp initialization
// ============================================================================

// SetQueryMode sets the query mode for the application (Solution 2)
func SetQueryMode(mode QueryMode) func(*BaseApp) {
	return func(app *BaseApp) {
		// TODO: Set app.queryConfig.Mode = mode
	}
}

// EnableAsyncQueryStore enables the async query store (Solution 3)
func EnableAsyncQueryStore(lagBlocks int) func(*BaseApp) {
	return func(app *BaseApp) {
		// TODO: Initialize app.asyncQueryStore
		// app.asyncQueryStore = NewAsyncQueryStore(app, app.logger, lagBlocks)
		// app.asyncQueryStore.Start()
	}
}

// EnableQueryCache enables query result caching (Solution 4)
func EnableQueryCache(ttl time.Duration) func(*BaseApp) {
	return func(app *BaseApp) {
		// TODO: Initialize app.queryCache
		// app.queryCache = NewQueryCache(ttl)
	}
}

// ============================================================================
// INTEGRATION POINT: Modified Query Handler
// ============================================================================

// This would replace handleQueryGRPC in abci.go
func (app *BaseApp) handleQueryGRPCOptimized(handler GRPCQueryHandler, req *abci.RequestQuery) *abci.ResponseQuery {
	// Check which solution is configured and route accordingly
	
	// SOLUTION 4: If caching is enabled, use cached handler
	// TODO: Check if app.queryCache is enabled
	// if app.queryCache != nil {
	//     return app.handleQueryGRPCWithCache(handler, req)
	// }
	
	// SOLUTION 3: If async store is enabled, use it
	// TODO: Check if app.asyncQueryStore is enabled
	// if app.asyncQueryStore != nil && app.asyncQueryStore.enabled.Load() {
	//     ctx, err := app.asyncQueryStore.GetQueryContext(req.Height, req.Prove)
	//     // ... handle query with async store context
	// }
	
	// SOLUTION 2: Check query mode
	// TODO: Check app.queryConfig.Mode
	// switch app.queryConfig.Mode {
	// case QueryModeFast:
	//     ctx, err := app.CreateQueryContextSolution1(req.Height, req.Prove)
	//     // ... handle query
	// case QueryModeEventual:
	//     ctx, err := app.CreateQueryContextSolution3(req.Height, req.Prove)
	//     // ... handle query
	// }
	
	// SOLUTION 1: Default to always using committed state
	ctx, err := app.CreateQueryContextSolution1(req.Height, req.Prove)
	if err != nil {
		return sdkerrors.QueryResult(err, app.trace)
	}
	
	resp, err := handler(ctx, req)
	if err != nil {
		resp = sdkerrors.QueryResult(gRPCErrorToSDKError(err), app.trace)
		resp.Height = req.Height
		return resp
	}
	
	return resp
}

// ============================================================================
// COMMIT INTEGRATION
// ============================================================================

// This shows where to integrate with the Commit method
func integrateWithCommit(app *BaseApp) {
	// After successful commit in baseapp.Commit():
	
	// SOLUTION 3: Signal async query store update
	// if app.asyncQueryStore != nil {
	//     app.asyncQueryStore.SignalUpdate()
	// }
	
	// SOLUTION 4: Clear query cache on commit (optional)
	// if app.queryCache != nil {
	//     app.queryCache.Clear() // Or implement partial invalidation
	// }
}

// ============================================================================
// CLIENT-SIDE WORKAROUND (No SDK changes needed)
// ============================================================================

// QueryWithLag queries at a specific number of blocks behind latest
func QueryWithLag(queryClient interface{}, lagBlocks int64) error {
	// This is a client-side workaround that doesn't require SDK changes
	// 1. Get current height
	// 2. Query at (current_height - lagBlocks)
	
	// Example pseudo-code:
	// currentHeight := queryClient.GetLatestHeight()
	// queryHeight := currentHeight - lagBlocks
	// if queryHeight < 1 {
	//     queryHeight = 1  
	// }
	// return queryClient.QueryAtHeight(queryHeight)
	
	// TODO: Implement actual client logic based on your query client type
	return nil
}