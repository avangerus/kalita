public class Expense
{
    public Guid Id { get; set; }
    public string Name { get; set; } = string.Empty;
    public decimal Amount { get; set; }
    public string Status { get; set; } = "Draft";
    public Guid EstimateId { get; set; }

    public List<EstimateLine> Lines { get; set; } = new();
}
