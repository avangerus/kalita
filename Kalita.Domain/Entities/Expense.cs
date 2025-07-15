namespace Kalita.Domain.Entities;

public class Expense : IWorkflowEntity
{
    public Guid Id { get; set; }
    public string Name { get; set; } = "";
    public decimal Amount { get; set; }
    public string Status { get; set; } = "";
    public Guid EstimateId { get; set; }
    public Estimate? Estimate { get; set; }

    public List<EstimateLine> Lines { get; set; } = new();
}