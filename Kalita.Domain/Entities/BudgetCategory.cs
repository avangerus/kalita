namespace Kalita.Domain.Entities;

public class BudgetCategory
{
    public Guid Id { get; set; }
    public string Name { get; set; } = string.Empty;
    public string? Code { get; set; }
}
