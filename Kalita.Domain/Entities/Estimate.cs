using Kalita.Domain.Entities;
public class Estimate : IWorkflowEntity
{
    public Guid Id { get; set; }
    public string Name { get; set; } = "";
    public decimal Amount { get; set; }
    public decimal Margin { get; set; }
    public string Status { get; set; } = "";
    public string CreatedByUserId { get; set; } = "";

    public List<Invoice> Invoices { get; set; } = new();
    public List<Expense> Expenses { get; set; } = new();
    public List<EstimateLine> EstimateLines { get; set; } = new();
}