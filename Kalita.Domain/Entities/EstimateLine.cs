using Kalita.Domain.Entities;
public class EstimateLine
{
    public Guid Id { get; set; }
    public string Name { get; set; } = "";
    public decimal Quantity { get; set; }
    public decimal Price { get; set; }
    public decimal Total => Quantity * Price;
    public EstimateLineType Type { get; set; }

    public Guid EstimateId { get; set; }
    public Estimate? Estimate { get; set; }

    public Guid? InvoiceId { get; set; }
    public Invoice? Invoice { get; set; }

    public Guid? ExpenseId { get; set; }
    public Expense? Expense { get; set; }
    public Guid? UnitId { get; set; }           // Ссылка на DictionaryItem
    public DictionaryItem? Unit { get; set; }   // Навигационное свойство (EF)
}
public enum EstimateLineType
{
    Income,
    Outcome
}
