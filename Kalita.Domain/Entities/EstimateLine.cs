public class EstimateLine
{
    public Guid Id { get; set; }
    public Guid EstimateId { get; set; }
    public string Name { get; set; }
    public decimal Amount { get; set; }
    public EstimateLineType Type { get; set; } // Income/Outcome

    // Новое:
    public Guid? InvoiceId { get; set; }   // Если линия привязана к счету (для Income)
    public Guid? ExpenseId { get; set; }   // Если линия привязана к расходу (для Outcome)
}
public enum EstimateLineType
{
    Income,
    Outcome
}
