public class ExpenseResponse
{
    public Guid Id { get; set; }
    public string Name { get; set; } = "";
    public string Status { get; set; } = "";
    public Estimate? Estimate { get; set; }
    public List<EstimateLine> Lines { get; set; } = new();
    // + другие свойства, если нужно
}