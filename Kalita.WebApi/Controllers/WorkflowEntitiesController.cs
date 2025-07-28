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
    public IActionResult Create(string entityType, [FromBody] object data)
    {
        var result = _service.Create(entityType, data);
        return Ok(result); // <= важно, чтобы здесь был объект (DTO), а не Ok() или Ok(null)
    }

    [HttpPost("{entityType}/{id}/transition")]
    public IActionResult Transition(string entityType, Guid id, [FromBody] TransitionRequest request)
    {
        string error;

        // Получаем сущность по id
        var entity = _service.Get(entityType, id);
        if (entity == null)
            return NotFound("Entity not found");

        // Здесь request.ActionCode, а не NextStatus!
        string actionCode = request.ActionCode ?? ""; // или request.Action, если так поле называется

        // Можно передавать data (если есть условия)
        if (_service.TryTransition(entityType, id, actionCode, entity, request.UserRole ?? "role:Test", out error))
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
    public string ActionCode { get; set; } 
}
