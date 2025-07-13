// Kalita.WebApi/Controllers/WorkflowEntitiesController.cs

using Microsoft.AspNetCore.Mvc;
using Kalita.Application.Services;
using Kalita.Domain.Entities;

namespace Kalita.WebApi.Controllers;

[ApiController]
[Route("api/[controller]")]
public class WorkflowEntitiesController : KalitaBaseController
{
    private readonly WorkflowEntityService _service;

    public WorkflowEntitiesController(WorkflowEntityService service)
    {
        _service = service;
    }

    [HttpGet("{entityType}")]
    public ActionResult<IEnumerable<object>> GetAll(string entityType)
        => Ok(_service.GetAll(entityType));

    [HttpGet("{entityType}/{id}")]
    public ActionResult<object> Get(string entityType, Guid id)
        => Ok(_service.Get(entityType, id));

    [HttpPost("{entityType}")]
    public IActionResult Create(string entityType, [FromBody] object entity)
    {
        // Для простоты — через специфичные сервисы или AddEntity(entityType, entity)
        // Можно расширить реализацию под разные типы сущностей
        return Ok();
    }

    [HttpPost("{entityType}/{id}/transition")]
    public IActionResult Transition(string entityType, Guid id, [FromBody] TransitionRequest request)
    {
        Guid userId = Guid.NewGuid();   // заменить на авторизацию, если нужно
        string userFio = "Test User";
        string error;
        if (_service.TryTransition(entityType, id, request.NextStatus, userId, userFio, request.Comment ?? "", request.UserRole ?? "role:Test", out error))
            return Ok();
        return BadRequest(error);
    }

    [HttpGet("{entityType}/{id}/history")]
    public ActionResult<List<WorkflowStepHistory>> GetHistory(string entityType, Guid id)
        => Ok(_service.GetHistory(entityType, id));
}

public class TransitionRequest
{
    public string NextStatus { get; set; } = "";
    public string? Comment { get; set; }
    public string? UserRole { get; set; }
}
