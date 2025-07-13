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
    public string Name { get; set; }
    public Guid EstimateId { get; set; }
    public Guid? ContractorId { get; set; }
    public Contractor? Contractor { get; set; }
    public List<EstimateLine> Lines { get; set; } = new(); // или IEnumerable, если нужно
}
