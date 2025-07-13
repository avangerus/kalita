using Microsoft.AspNetCore.Mvc;
using Kalita.Domain.Entities;
using Kalita.Application.Services;
using Kalita.Application.Models;

namespace Kalita.WebApi.Controllers;

[ApiController]
[Route("api/[controller]")]
public class EstimatesController : KalitaBaseController
{
    private readonly EstimateService _service;
    public EstimatesController(EstimateService service) => _service = service;

    // [HttpGet]
    // public ActionResult<List<Estimate>> Get() => _service.GetEstimates();

    [HttpGet("{id}")]
    public ActionResult<Estimate?> Get(Guid id) => _service.GetEstimate(id);


    [HttpPost]
    public IActionResult Create([FromBody] CreateEstimateRequest req)
    {
        // Подставляем текущего пользователя (UserId из базового контроллера)
        var estimate = _service.CreateEstimate(req.Name, req.Amount, req.Margin, UserId!);
        return Ok(estimate);
    }

    [HttpGet("/api/projects/{projectId}/estimates")]
    public IActionResult GetByProject(Guid projectId)
    {
        var estimates = _service.GetByProject(projectId);
        return Ok(estimates);
    }

    [HttpGet("{estimateId}/report")]
    public IActionResult GetReport(Guid estimateId)
    {
        var report = _service.GetReport(estimateId);
        return Ok(report);
    }



    [HttpGet]
    public IActionResult GetAll()
    {
        var query = _service.Query();
        // Проверяем роль
        if (UserRole == "User" || UserRole == "Contractor" || UserRole == "Employee")
            query = query.Where(x => x.CreatedByUserId == UserId);

        var result = query.ToList();
        return Ok(result);
    }

    [HttpPut("{id}")]
    public IActionResult Update(Guid id, [FromBody] Estimate updated)
    {
        var estimate = _service.GetEstimate(id);
        if (estimate == null)
            return NotFound();

        // Пример: обновим только нужные поля
        estimate.Margin = updated.Margin;
        estimate.Amount = updated.Amount;
        estimate.Name = updated.Name ?? estimate.Name;
        // ...добавь по необходимости

        _service.Update(estimate); // Реализуй Update в сервисе если его нет, или сохраняй напрямую в БД

        return Ok(estimate);
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
