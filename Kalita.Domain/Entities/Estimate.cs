public class Estimate
{
    public Guid Id { get; set; }
    public string Name { get; set; } = string.Empty;
    public decimal Amount { get; set; }
    public decimal Margin { get; set; }
    public string Status { get; set; } = "Draft";
    public string CreatedByUserId { get; set; } = "";
    public Guid ProjectId { get; set; }
}
