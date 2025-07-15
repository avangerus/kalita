using Kalita.Domain.Entities;
using Kalita.Infrastructure.Persistence;
using Kalita.Application.DTO;

public class InvoiceService
{
    private readonly AppDbContext _db;
    public InvoiceService(AppDbContext db) => _db = db;

    // DTO для фронта с деталями
    public InvoiceResponse? GetInvoiceWithDetails(Guid id)
    {
        var invoice = _db.Invoices.FirstOrDefault(x => x.Id == id);
        if (invoice == null) return null;
        // подгрузи estimate, линии и др. если надо
        var estimate = _db.Estimates.FirstOrDefault(e => e.Id == invoice.EstimateId);
        var lines = _db.EstimateLines.Where(l => l.InvoiceId == id).ToList();
        // Optionally подтяни имена из справочников и т.д.
        return new InvoiceResponse
        {
            Id = invoice.Id,
            Name = invoice.Name,
            Status = invoice.Status,
            Estimate = estimate, // или EstimateDto если нужно
            Lines = lines,       // или LineDto
            // ...
        };
    }

    // Остальные методы по необходимости (Create, Update, Delete)
}
