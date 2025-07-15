using Microsoft.AspNetCore.Mvc;
using Kalita.Domain.Entities;
using Kalita.Application.Services;
using Kalita.WebApi.DTO;

namespace Kalita.WebApi.Controllers;

[ApiController]
[Route("api/[controller]")]
public class InvoicesController : KalitaBaseController
{
    private readonly InvoiceService _service;
    private readonly WorkflowEntityService _workflowService;

    public InvoicesController(InvoiceService service, WorkflowEntityService workflowService)
    {
        _service = service;
        _workflowService = workflowService;
    }



    // Получить все счета
    [HttpGet]
    public ActionResult<List<Invoice>> Get()
    {
        var invoices = _workflowService.GetAll("Invoice").Cast<Invoice>().ToList();
        return Ok(invoices);
    }

    [HttpGet("{id}")]
    public IActionResult GetOne(Guid id)
    {
        var result = _service.GetInvoiceWithDetails(id);
        if (result == null) return NotFound();
        return Ok(result);
    }

    // Создать счет
    [HttpPost]
    public IActionResult Create([FromBody] CreateInvoiceRequest request)
    {
        var result = _workflowService.CreateInvoice(
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

    // Все счета по смете
    [HttpGet("/api/estimates/{estimateId}/invoices")]
    public IActionResult GetByEstimate(Guid estimateId)
    {
        var invoices = _workflowService.GetInvoicesByEstimate(estimateId);
        return Ok(invoices);
    }

    // Все строки по счету (EstimateLines)
    [HttpGet("{id}/lines")]
    public IActionResult GetLines(Guid id)
    {
        var lines = _workflowService.GetLinesByInvoice(id);
        return Ok(lines);
    }

    // Переход по маршруту
    [HttpPost("{id}/transition")]
    public IActionResult Transition(Guid id, [FromBody] TransitionRequest request)
    {
        string error;
        if (_workflowService.TryTransition(
                "Invoice",
                id,
                request.NextStatus,
                UserId!,
                UserFio!,
                request.Comment ?? "",
                request.UserRole,
                out error))
            return Ok();
        return BadRequest(error);
    }

    // История маршрута
    [HttpGet("{id}/history")]
    public ActionResult<List<WorkflowStepHistory>> GetHistory(Guid id) =>
        _workflowService.GetHistory("Invoice", id);
}
