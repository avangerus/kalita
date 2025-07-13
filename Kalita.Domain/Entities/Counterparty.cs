namespace Kalita.Domain.Entities;

public class Counterparty
{
    public Guid Id { get; set; }
    public string Name { get; set; } = string.Empty;
    public string INN { get; set; } = string.Empty;
    public string KPP { get; set; } = string.Empty;
    public string AccountNumber { get; set; } = string.Empty;
    public string ContactPerson { get; set; } = string.Empty;
    public string Email { get; set; } = string.Empty;
}
