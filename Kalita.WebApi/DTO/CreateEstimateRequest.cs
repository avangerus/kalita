public class CreateEstimateRequest
{
    public string Name { get; set; } = "";
    public decimal Amount { get; set; }
    public decimal Margin { get; set; }
    // Можно добавить другие поля, кроме Id и CreatedByUserId
}