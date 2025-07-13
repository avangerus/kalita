namespace Kalita.Domain.Entities;

public class Invoice
{
    public Guid Id { get; set; }
    public Guid ExpenseId { get; set; }
    public Expense? Expense { get; set; }
    public string Number { get; set; } = string.Empty;
    public decimal Amount { get; set; }
    public DateTime IssuedAt { get; set; }
    public string Status { get; set; } = "Draft";
    public List<WorkflowStepHistory> WorkflowHistory { get; set; } = new();
}
