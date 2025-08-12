package baseapp

import (
	"time"
)

// ============================================================================
// EXAMPLE: How to integrate query optimizations into your app
// ============================================================================

// Example 1: Quick fix - Modify existing CreateQueryContext (SOLUTION 1)
// In your baseapp/abci.go, replace the CreateQueryContext method:
func ExampleQuickFix(app *BaseApp) {
	// Replace this line in handleQueryGRPC (baseapp/abci.go:1153):
	// ctx, err := app.CreateQueryContext(req.Height, req.Prove)
	
	// With this (SOLUTION 1 - always use committed state):
	// ctx, err := app.createQueryContextCommittedOnly(req.Height, req.Prove)
}

// Example 2: App initialization with query optimizations
func ExampleAppWithOptimizations() *BaseApp {
	// Create base app with optimizations
	app := NewBaseApp(
		"myapp",
		logger,
		db,
		txDecoder,
		// SOLUTION 2: Enable fast query mode
		SetQueryMode(QueryModeFast),
		
		// SOLUTION 3: Enable async query store with 1 block lag
		EnableAsyncQueryStore(1),
		
		// SOLUTION 4: Enable query cache with 1 second TTL
		EnableQueryCache(1 * time.Second),
	)
	
	return app
}

// Example 3: Custom app with selective optimizations
type MyApp struct {
	*BaseApp
	// ... your app fields
}

func NewMyApp() *MyApp {
	baseApp := NewBaseApp("myapp", logger, db, txDecoder)
	
	// Configure query optimizations based on your needs
	config := QueryConfig{
		// SOLUTION 2: Use fast mode for production
		Mode: QueryModeFast,
		
		// SOLUTION 3: Enable async store for read-heavy apps
		EnableAsyncQueryStore: true,
		QueryStoreLagBlocks:   1, // Lag 1 block behind for consistency
		
		// SOLUTION 4: Enable caching for repetitive queries
		EnableQueryCache: true,
		QueryCacheTTL:    500 * time.Millisecond, // Cache for half a block time
	}
	
	// Apply configuration
	baseApp.ConfigureQueryOptimizations(config)
	
	return &MyApp{
		BaseApp: baseApp,
	}
}

// Example 4: Minimal code change in existing abci.go
func ExampleMinimalChange() {
	// In baseapp/abci.go, modify CreateQueryContextWithCheckHeader:
	// Add this at the beginning of the function (around line 1209):
	
	/*
	// SOLUTION 1: Quick fix - always use committed state for latest queries
	if height == 0 && !checkHeader {
		qms := app.qms
		if qms == nil {
			qms = app.cms.(storetypes.MultiStore)
		}
		height = qms.LatestVersion()
		// Continue with specific height logic...
	}
	*/
}

// Example 5: Client-side workaround (no SDK changes needed)
func ExampleClientSideWorkaround() {
	// In your client application:
	/*
	import (
		"context"
		grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	)
	
	func QueryWithoutBlocking(clientCtx client.Context, req interface{}) error {
		// Get current height
		statusClient := cmtservice.NewServiceClient(clientCtx)
		status, err := statusClient.GetNodeInfo(context.Background(), &cmtservice.GetNodeInfoRequest{})
		if err != nil {
			return err
		}
		
		currentHeight := status.DefaultNodeInfo.Network
		
		// Query at previous block to avoid in-progress state
		queryHeight := currentHeight - 1
		if queryHeight < 1 {
			queryHeight = 1
		}
		
		// Add height to context
		ctx := context.Background()
		ctx = grpctypes.SetGRPCBlockHeight(ctx, queryHeight)
		
		// Execute query at specific height
		// ... your query logic here ...
		
		return nil
	}
	*/
}

// Example 6: Testing heavy EndBlocker scenario
func ExampleTestingSetup() {
	// Create app with optimizations for testing
	app := NewBaseApp(
		"testapp",
		logger,
		db, 
		txDecoder,
		SetQueryMode(QueryModeFast), // SOLUTION 2: Enable fast queries
	)
	
	// Set a heavy EndBlocker
	app.SetEndBlocker(func(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock {
		// Simulate heavy computation
		time.Sleep(5 * time.Second)
		
		// Your actual EndBlock logic
		return abci.ResponseEndBlock{}
	})
	
	// Queries will now use committed state and won't be blocked by the heavy EndBlocker
}

// Example 7: Production configuration
func ExampleProductionConfig() QueryConfig {
	return QueryConfig{
		// SOLUTION 2: Fast mode for production
		Mode: QueryModeFast,
		
		// SOLUTION 3: Async store with conservative lag
		EnableAsyncQueryStore: true,
		QueryStoreLagBlocks:   2, // 2 blocks behind for safety
		
		// SOLUTION 4: Aggressive caching for read-heavy apps
		EnableQueryCache: true,
		QueryCacheTTL:    2 * time.Second, // Adjust based on block time
	}
}

// Example 8: Gradual rollout strategy
func ExampleGradualRollout() {
	// Phase 1: Start with Solution 1 only (minimal change)
	// Just modify CreateQueryContext to always use committed state
	
	// Phase 2: Add configuration (Solution 2)
	// Allow switching between modes via config/flags
	
	// Phase 3: Add caching (Solution 4)
	// Enable for specific query types that repeat often
	
	// Phase 4: Full async store (Solution 3)
	// Enable for maximum performance once tested
}

// ============================================================================
// INTEGRATION CHECKLIST
// ============================================================================

/*
STEP-BY-STEP INTEGRATION:

1. IMMEDIATE FIX (5 minutes):
   □ Locate baseapp/abci.go
   □ Find CreateQueryContextWithCheckHeader function (line ~1208)
   □ Add the Solution 1 code at the start of height==0 handling
   □ Test with your heavy EndBlocker

2. CONFIGURABLE FIX (30 minutes):
   □ Add QueryConfig struct to your app
   □ Add query mode flag to your app CLI
   □ Implement Solution 2 routing logic
   □ Test different modes

3. CACHING LAYER (1 hour):
   □ Add QueryCache to BaseApp
   □ Implement cache in handleQueryGRPC
   □ Add cache invalidation in Commit
   □ Monitor cache hit rates

4. ASYNC STORE (2-4 hours):
   □ Implement AsyncQueryStore
   □ Add update logic to Commit
   □ Test consistency guarantees
   □ Monitor lag and performance

TESTING CHECKLIST:
□ Heavy EndBlocker doesn't block queries
□ Query results are consistent
□ No data races under load
□ Performance improvement measured
□ Cache invalidation works correctly
□ Async store stays synchronized

MONITORING:
□ Query latency percentiles (p50, p95, p99)
□ Cache hit rate (if using Solution 4)
□ Async store lag (if using Solution 3)
□ Query error rates
□ Memory usage (especially with caching)
*/