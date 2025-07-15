namespace Kalita.Application.DTO;

public class InvoiceResponse
{
    public Guid Id { get; set; }
    public string Name { get; set; } = "";
    public string Status { get; set; } = "";
    public Estimate? Estimate { get; set; }
    public List<EstimateLine> Lines { get; set; } = new();
    // ... всё, что нужно фронту
}

