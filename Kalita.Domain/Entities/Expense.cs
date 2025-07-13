public class Expense
{
    public Guid Id { get; set; }
    public string Name { get; set; } = string.Empty;
    public decimal Amount { get; set; }
    public string Status { get; set; } = "Draft";
    // Можно добавить другие поля, если хочешь
}
