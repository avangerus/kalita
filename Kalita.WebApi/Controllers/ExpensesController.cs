using Microsoft.AspNetCore.Mvc;
using Kalita.Domain.Entities;
using Kalita.Application.Services;
using Kalita.Application.Models;

namespace Kalita.WebApi.Controllers;

[ApiController]
[Route("api/[controller]")]
public class ExpensesController : ControllerBase
{
    private readonly ExpenseService _service;
    public ExpensesController(ExpenseService service) => _service = service;

    [HttpGet]
    public ActionResult<List<Expense>> Get() => _service.GetExpenses();

    [HttpGet("{id}")]
    public ActionResult<Expense?> Get(Guid id) => _service.GetExpense(id);

    [HttpPost]
    public IActionResult Create([FromBody] Expense expense)
    {
        _service.CreateExpense(expense);
        return Ok();
    }

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

    [HttpGet("{id}/history")]
    public ActionResult<List<WorkflowStepHistory>> GetHistory(Guid id) =>
        _service.GetHistory(id);
}


