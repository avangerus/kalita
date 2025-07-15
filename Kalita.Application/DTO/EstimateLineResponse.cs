namespace Kalita.Application.DTO;

public class EstimateLineResponse
{
    public Guid Id { get; set; }
    public string Name { get; set; } = "";
    public decimal Quantity { get; set; }
    public decimal Price { get; set; }
    public decimal Total { get; set; }
    public string Type { get; set; } = "";

    public Guid? UnitId { get; set; }
    public string? UnitName { get; set; }
}