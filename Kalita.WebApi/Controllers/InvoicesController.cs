using Microsoft.AspNetCore.Mvc;
using Kalita.Domain.Entities;
using Kalita.Application.Services;
using Kalita.Application.Models;
using Kalita.WebApi.DTO;

namespace Kalita.WebApi.Controllers;

[ApiController]
[Route("api/[controller]")]
public class InvoicesController : KalitaBaseController
{
    private readonly InvoiceService _service;
    public InvoicesController(InvoiceService service) => _service = service;

    [HttpGet]
    public ActionResult<List<Invoice>> Get() => _service.GetInvoices();

    [HttpGet("{id}")]
    public ActionResult<Invoice?> Get(Guid id) => _service.GetInvoice(id);

    [HttpPost]
    public IActionResult Create([FromBody] CreateInvoiceRequest request)
    {
        var result = _service.CreateInvoice(
            request.Name,
            request.Amount,
            request.Status,
            request.EstimateId,
            request.LineIds
        );

        if (!result.Success)
            return BadRequest(result.Error);

        return Ok(result.Invoice);
    }

    [HttpGet("/api/estimates/{estimateId}/invoices")]
    public IActionResult GetByEstimate(Guid estimateId)
    {
        var invoices = _service.GetByEstimate(estimateId); // Реализуй в InvoiceService
        return Ok(invoices);
    }

    [HttpGet("{invoiceId}/lines")]
    public IActionResult GetLines(Guid invoiceId)
    {
        var lines = _service.GetLines(invoiceId);
        return Ok(lines);
    }


    [HttpPost("{id}/transition")]
    public IActionResult Transition(Guid id, [FromBody] TransitionRequest request)
    {
        Guid userId = Guid.NewGuid();
        string userFio = "Test User";
        string error;

        if (_service.TryTransition(
            id,
            request.NextStatus,
            userId,
            userFio,
            request.Comment ?? "",
            request.UserRole,
            out error))
            return Ok();
        return BadRequest(error);
    }

    [HttpGet("{id}/history")]
    public ActionResult<List<WorkflowStepHistory>> GetHistory(Guid id) =>
        _service.GetHistory(id);
}


