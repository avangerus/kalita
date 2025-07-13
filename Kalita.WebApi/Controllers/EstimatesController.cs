using Microsoft.AspNetCore.Mvc;
using Kalita.Domain.Entities;
using Kalita.Application.Services;
using Kalita.Application.Models;

namespace Kalita.WebApi.Controllers;

[ApiController]
[Route("api/[controller]")]
public class EstimatesController : ControllerBase
{
    private readonly EstimateService _service;
    public EstimatesController(EstimateService service) => _service = service;

    [HttpGet]
    public ActionResult<List<Estimate>> Get() => _service.GetEstimates();

    [HttpGet("{id}")]
    public ActionResult<Estimate?> Get(Guid id) => _service.GetEstimate(id);

    [HttpPost]
    public IActionResult Create([FromBody] Estimate estimate)
    {
        _service.CreateEstimate(estimate);
        return Ok();
    }

    // Новый эндпоинт для перехода по маршруту
    [HttpPost("{id}/transition")]
    public IActionResult Transition(Guid id, [FromBody] TransitionRequest request)
    {
        Guid userId = Guid.NewGuid();
        string userFio = "Test User";
        string error;

        // Не забывай передавать все параметры, включая out error!
        if (_service.TryTransition(
            id,
            request.NextStatus,
            userId,
            userFio,
            request.Comment ?? "",
            request.UserRole,   // <-- если добавил проверку по роли
            out error))
            return Ok();
        return BadRequest(error);
    }

    // Для истории
    [HttpGet("{id}/history")]
    public ActionResult<List<WorkflowStepHistory>> GetHistory(Guid id) =>
        _service.GetHistory(id);
}

// DTO для перехода
