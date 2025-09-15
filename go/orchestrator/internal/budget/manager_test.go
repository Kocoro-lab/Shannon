package budget

import (
    "context"
    "database/sql"
    "regexp"
    "testing"
    "time"

    sqlmock "github.com/DATA-DOG/go-sqlmock"
    "go.uber.org/zap"
)

func TestCheckBudget_DefaultsAllowSmallEstimate(t *testing.T) {
    bm := NewBudgetManager(&sql.DB{}, zap.NewNop())
    ctx := context.Background()
    res, err := bm.CheckBudget(ctx, "u1", "s1", "t1", 1000)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if !res.CanProceed {
        t.Fatalf("expected CanProceed=true, got false: %+v", res)
    }
    if res.RemainingTaskBudget <= 0 || res.RemainingSessionBudget <= 0 {
        t.Fatalf("expected positive remaining budgets, got %+v", res)
    }
}

func TestEstimateCost_ModelPricing(t *testing.T) {
    bm := NewBudgetManager(&sql.DB{}, zap.NewNop())
    cost := bm.estimateCost(1000, "gpt-3.5-turbo")
    if cost <= 0 {
        t.Fatalf("expected positive cost for 1k tokens, got %f", cost)
    }
}

func TestRecordUsage_ExecInsertsTokenUsage(t *testing.T) {
    db, mock, err := sqlmock.New()
    if err != nil { t.Fatalf("sqlmock: %v", err) }
    defer db.Close()

    bm := NewBudgetManager(db, zap.NewNop())
    usage := &BudgetTokenUsage{
        UserID: "u1", SessionID: "s1", TaskID: "t1", AgentID: "a1",
        Model: "gpt-3.5-turbo", Provider: "openai", InputTokens: 10, OutputTokens: 20,
    }

    mock.ExpectExec(regexp.QuoteMeta(
        "INSERT INTO token_usage (\n\t\tuser_id, task_id, provider, model,\n\t\tprompt_tokens, completion_tokens, total_tokens, cost_usd\n\t\t) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
    )).WithArgs(
        usage.UserID, usage.TaskID, usage.Provider, usage.Model,
        sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
    ).WillReturnResult(sqlmock.NewResult(1, 1))

    if err := bm.RecordUsage(context.Background(), usage); err != nil {
        t.Fatalf("RecordUsage error: %v", err)
    }
    if err := mock.ExpectationsWereMet(); err != nil {
        t.Fatalf("unmet expectations: %v", err)
    }
}

func TestGetUsageReport_AggregatesRows(t *testing.T) {
    db, mock, err := sqlmock.New()
    if err != nil { t.Fatalf("sqlmock: %v", err) }
    defer db.Close()

    bm := NewBudgetManager(db, zap.NewNop())

    rows := sqlmock.NewRows([]string{"user_id","task_id","model","provider","input_total","output_total","total_tokens","total_cost","request_count"}).
        AddRow("u1","t1","gpt-3.5-turbo","openai", 30, 60, 90, 0.1, 2)

    mock.ExpectQuery(regexp.QuoteMeta("SELECT user_id, task_id, model, provider")).
        WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
        WillReturnRows(rows)

    from := time.Now().Add(-time.Hour)
    to := time.Now()
    rep, err := bm.GetUsageReport(context.Background(), UsageFilters{StartTime: from, EndTime: to})
    if err != nil { t.Fatalf("GetUsageReport error: %v", err) }
    if rep.TotalTokens != 90 || rep.TotalCostUSD <= 0 {
        t.Fatalf("unexpected report: %+v", rep)
    }
}

// no additional helpers
