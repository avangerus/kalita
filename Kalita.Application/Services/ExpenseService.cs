using Kalita.Domain.Entities;
using Kalita.Application.Workflow;
using Kalita.Infrastructure.Persistence;

namespace Kalita.Application.Services;

public class ExpenseService
{
    private readonly AppDbContext _db;
    private readonly WorkflowEngine _workflow;

    public ExpenseService(AppDbContext db, WorkflowEngine workflow)
    {
        _db = db;
        _workflow = workflow;
    }

    public List<Expense> GetExpenses() => _db.Expenses.ToList();

    public Expense? GetExpense(Guid id) => _db.Expenses.FirstOrDefault(x => x.Id == id);

    public void CreateExpense(Expense expense)
    {
        _db.Expenses.Add(expense);
        _db.SaveChanges();
    }

    public bool TryTransition(Guid entityId, string nextStatus, Guid userId, string userFio, string comment, string userRole, out string error)
    {
        var expense = _db.Expenses.FirstOrDefault(x => x.Id == entityId);
        if (expense == null)
        {
            error = "Expense not found.";
            return false;
        }

        if (!_workflow.CanTransition(expense.Status, nextStatus, expense, userRole))
        {
            error = "Transition not allowed (wrong role or condition).";
            return false;
        }

        var history = new WorkflowStepHistory
        {
            Id = Guid.NewGuid(),
            EntityId = expense.Id, // если хочешь сделать ExpenseId — добавь поле в WorkflowStepHistory
            EntityType = "expense",
            StepName = _workflow.GetCurrentStep(expense.Status)?.Name ?? expense.Status,
            Status = nextStatus,
            UserId = userId,
            UserFio = userFio,
            DateTime = DateTime.UtcNow,
            Action = "Transition",
            Comment = comment,
            Result = "Success"
        };
        _db.WorkflowStepHistories.Add(history);
        expense.Status = nextStatus;

        _db.SaveChanges();
        error = "";
        return true;
    }

    public List<WorkflowStepHistory> GetHistory(Guid expenseId)
    {
        return _db.WorkflowStepHistories
            .Where(h => h.EntityId == expenseId) // Если у тебя WorkflowStepHistory не различает Expense/Estimate, можно добавить поле EntityType
            .OrderBy(h => h.DateTime)
            .ToList();
    }
}
