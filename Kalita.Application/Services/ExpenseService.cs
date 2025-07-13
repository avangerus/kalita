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
    public List<Expense> GetByEstimate(Guid estimateId)
{
    return _db.Expenses.Where(e => e.EstimateId == estimateId).ToList();
}


    public Expense? GetExpense(Guid id) => _db.Expenses.FirstOrDefault(x => x.Id == id);


        public (bool Success, Expense? Expense, string? Error) CreateExpense(
        string name,
        decimal amount,
        string status,
        Guid estimateId,
        List<Guid> lineIds)
    {
        // Проверка сметы
        var estimate = _db.Estimates.FirstOrDefault(e => e.Id == estimateId);
        if (estimate == null)
            return (false, null, "Смета не найдена");

        // Проверка линий Outcome
        var lines = _db.EstimateLines
            .Where(l => lineIds.Contains(l.Id) && l.Type == EstimateLineType.Outcome)
            .ToList();

        if (lines.Count != lineIds.Count)
            return (false, null, "Некоторые позиции не найдены или не типа Outcome");

        foreach (var line in lines)
        {
            if (line.ExpenseId != null)
                return (false, null, $"Позиция {line.Id} уже привязана к другому расходу");
        }

        // Создание расхода
        var expense = new Expense
        {
            Id = Guid.NewGuid(),
            Name = name,
            Amount = amount,
            Status = status,
            EstimateId = estimateId,
        };

        foreach (var line in lines)
        {
            line.ExpenseId = expense.Id;
            expense.Lines.Add(line);
        }

        _db.Expenses.Add(expense);
        _db.SaveChanges();

        return (true, expense, null);
    }

    public void Update(Estimate estimate)
    {
        _db.Estimates.Update(estimate);
        _db.SaveChanges();
    }
    public bool TryTransition(Guid entityId, string nextStatus, Guid userId, string userFio, string comment, string userRole, out string error)
    {
        var expense = _db.Expenses.FirstOrDefault(x => x.Id == entityId);
        var entityType = "Expense";
        if (expense == null)
        {
            error = "Expense not found.";
            return false;
        }

        if (!_workflow.CanTransition(entityType, expense.Status, nextStatus, expense, userRole))
        {
            error = "Transition not allowed (wrong role or condition).";
            return false;
        }

        var history = new WorkflowStepHistory
        {
            Id = Guid.NewGuid(),
            EntityId = expense.Id, // если хочешь сделать ExpenseId — добавь поле в WorkflowStepHistory
            EntityType = "expense",
            StepName = _workflow.GetCurrentStep(entityType, expense.Status)?.Name,
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
