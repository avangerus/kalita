using Kalita.Domain.Entities;
using Kalita.Application.Workflow;
using Kalita.Infrastructure.Persistence;


namespace Kalita.Application.Services;

public class InvoiceService
{
    private readonly AppDbContext _db;
    private readonly WorkflowEngine _workflow;

    public InvoiceService(AppDbContext db, WorkflowEngine workflow)
    {
        _db = db;
        _workflow = workflow;
    }

    public List<Invoice> GetInvoices() => _db.Invoices.ToList();
    public List<Invoice> GetByEstimate(Guid estimateId)
    {
        return _db.Invoices.Where(i => i.EstimateId == estimateId).ToList();
    }
    public List<EstimateLine> GetLines(Guid invoiceId)
    {
        return _db.EstimateLines.Where(l => l.InvoiceId == invoiceId).ToList();
    }


    public Invoice? GetInvoice(Guid id) => _db.Invoices.FirstOrDefault(x => x.Id == id);

    // public void CreateInvoice(Invoice invoice)
    // {
    //     _db.Invoices.Add(invoice);
    //     _db.SaveChanges();
    // }
    public (bool Success, Invoice? Invoice, string? Error) CreateInvoice(
            string name,
            decimal amount,
            string status,
            Guid estimateId,
            List<Guid> lineIds)
    {
        // Проверка сметы
        var estimate = _db.Estimates.FirstOrDefault(e => e.Id == estimateId);
        if (estimate == null)
            return (false, null, "Смета не найдена");

        // Проверка линий
        var lines = _db.EstimateLines
            .Where(l => lineIds.Contains(l.Id) && l.Type == EstimateLineType.Income)
            .ToList();

        if (lines.Count != lineIds.Count)
            return (false, null, "Некоторые позиции не найдены или не типа Income");

        foreach (var line in lines)
        {
            if (line.InvoiceId != null)
                return (false, null, $"Позиция {line.Id} уже привязана к другому счету");
        }

        // Создание счета
        var invoice = new Invoice
        {
            Id = Guid.NewGuid(),
            Name = name,
            Amount = amount,
            Status = status,
            EstimateId = estimateId,
        };

        // Привязка линий
        foreach (var line in lines)
        {
            line.InvoiceId = invoice.Id;
            invoice.Lines.Add(line);
        }

        _db.Invoices.Add(invoice);
        _db.SaveChanges();

        return (true, invoice, null);
    }

    public bool TryTransition(Guid entityId, string nextStatus, Guid userId, string userFio, string comment, string userRole, out string error)
    {
        var invoice = _db.Invoices.FirstOrDefault(x => x.Id == entityId);

        var entityType = "Invoice";

        if (invoice == null)
        {
            error = "Invoice not found.";
            return false;
        }

        //if (!_workflow.CanTransition(invoice.Status, nextStatus, invoice, userRole))
        if (!_workflow.CanTransition(entityType, invoice.Status, nextStatus, invoice, userRole))
        {
            error = "Transition not allowed (wrong role or condition).";
            return false;
        }

        var history = new WorkflowStepHistory
        {
            Id = Guid.NewGuid(),
            EntityId = invoice.Id,
            EntityType = "invoice",
            StepName = _workflow.GetCurrentStep(entityType, invoice.Status)?.Name,
            Status = nextStatus,
            UserId = userId,
            UserFio = userFio,
            DateTime = DateTime.UtcNow,
            Action = "Transition",
            Comment = comment,
            Result = "Success"
        };
        _db.WorkflowStepHistories.Add(history);
        invoice.Status = nextStatus;

        _db.SaveChanges();
        error = "";
        return true;
    }

    public List<WorkflowStepHistory> GetHistory(Guid invoiceId)
    {
        return _db.WorkflowStepHistories
            .Where(h => h.EntityId == invoiceId) // Если у тебя WorkflowStepHistory не различает Invoice/Estimate, можно добавить поле EntityType
            .OrderBy(h => h.DateTime)
            .ToList();
    }
}
