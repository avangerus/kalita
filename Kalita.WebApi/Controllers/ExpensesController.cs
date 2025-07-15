using Microsoft.AspNetCore.Mvc;
using Kalita.Domain.Entities;
using Kalita.Application.Services;
using Kalita.WebApi.DTO;

namespace Kalita.WebApi.Controllers;

[ApiController]
[Route("api/[controller]")]
public class ExpensesController : KalitaBaseController
{
    private readonly ExpenseService _service;
    private readonly WorkflowEntityService _workflowService;

    public ExpensesController(ExpenseService service, WorkflowEntityService workflowService)
    {
        _service = service;
        _workflowService = workflowService;
    }

    [HttpGet("{id}")]
    public IActionResult GetOne(Guid id)
    {
        var result = _service.GetExpenseWithDetails(id);
        if (result == null) return NotFound();
        return Ok(result);
    }





    // Получить все расходы (или фильтровать по пользователю/роли, если нужно)
    [HttpGet]
    public ActionResult<List<Expense>> Get()
    {
        var expenses = _workflowService.GetAll("Expense").Cast<Expense>().ToList();
        return Ok(expenses);
    }



    // Получить все расходы по смете
    [HttpGet("/api/estimates/{estimateId}/expenses")]
    public IActionResult GetByEstimate(Guid estimateId)
    {
        var expenses = _workflowService.GetExpensesByEstimate(estimateId);
        return Ok(expenses);
    }

    // Создать расход
    [HttpPost]
    public IActionResult Create([FromBody] CreateExpenseRequest request)
    {
        // Можно реализовать CreateExpense в WorkflowEntityService (или оставить временно отдельный сервис)
        // Ниже пример если есть такой метод:
        var result = _workflowService.CreateExpense(
            request.Name,
            request.Amount,
            request.Status,
            request.EstimateId,
            request.LineIds
        );

        if (!result.Success)
            return BadRequest(result.Error);

        return Ok(result.Expense);
    }

    // Получить все строки расходов (EstimateLine) для расхода
    [HttpGet("{id}/lines")]
    public IActionResult GetLines(Guid id)
    {
        var lines = _workflowService.GetLinesByExpense(id);
        return Ok(lines);
    }

    // Перевести по маршруту
    [HttpPost("{id}/transition")]
    public IActionResult Transition(Guid id, [FromBody] TransitionRequest request)
    {
        // Здесь UserId, UserFio бери из базового контроллера!
        string error;

        if (_workflowService.TryTransition(
                "Expense",
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



    [HttpGet("{id}/history")]
    public ActionResult<List<WorkflowStepHistory>> GetHistory(Guid id) =>
        _workflowService.GetHistory("Expense", id);
}
