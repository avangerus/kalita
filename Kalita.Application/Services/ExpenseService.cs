using Kalita.Domain.Entities;
using Kalita.Infrastructure.Persistence;
using Kalita.Application.DTO;

public class ExpenseService
{
    private readonly AppDbContext _db;
    public ExpenseService(AppDbContext db) => _db = db;

    public ExpenseResponse? GetExpenseWithDetails(Guid id)
    {
        var expense = _db.Expenses.FirstOrDefault(x => x.Id == id);
        if (expense == null) return null;
        var estimate = _db.Estimates.FirstOrDefault(e => e.Id == expense.EstimateId);
        var lines = _db.EstimateLines.Where(l => l.ExpenseId == id).ToList();
        return new ExpenseResponse
        {
            Id = expense.Id,
            Name = expense.Name,
            Status = expense.Status,
            Estimate = estimate,
            Lines = lines,
            // Добавь что надо!
        };
    }
    // Остальной CRUD — если надо!
}
