// using Microsoft.AspNetCore.Mvc;
// using Kalita.Application.Services;
// using Kalita.Infrastructure.Persistence;
// using Kalita.Domain.Entities;
// using Kalita.WebApi.DTO;
// using Kalita.Application.Workflow;

// namespace Kalita.WebApi.Controllers
// {
//     [ApiController]
//     [Route("api/[controller]")]
//     public class EstimatesController : KalitaBaseController
//     {
//         private readonly AppDbContext _db;
//         private readonly WorkflowEntityService _workflowService;

//         private readonly WorkflowEngine _workflow;

//         public EstimatesController(
//             AppDbContext db,
//             WorkflowEntityService workflowService,
//             WorkflowEngine workflow)
//         {
//             _db = db;
//             _workflowService = workflowService;
//             _workflow = workflow;
//         }

//         // Создание сметы — НЕ через _workflowService!
//         [HttpPost]
//         public IActionResult Create([FromBody] CreateEstimateRequest req)
//         {
//             var estimate = new Estimate
//             {
//                 Id = Guid.NewGuid(),
//                 Name = req.Name,
//                 Amount = req.Amount,
//                 Margin = req.Margin,
//                 Status = "Draft",
//                 CreatedByUserId = UserId!
//             };
//             _db.Estimates.Add(estimate);
//             _db.SaveChanges();
//             return Ok(estimate);
//         }

//         // Получить одну смету
//         [HttpGet("{id}")]
//         public IActionResult Get(Guid id)
//         {
//             var estimate = _workflowService.Get("Estimate", id) as Estimate;
//             if (estimate == null)
//                 return NotFound();
//             return Ok(estimate);
//         }

//         // Получить все сметы
//         [HttpGet]
//         public IActionResult GetAll()
//         {
//             var estimates = _workflowService.GetAll("Estimate")
//                                             .Cast<Estimate>()
//                                             .ToList();
//             return Ok(estimates);
//         }

//         [HttpGet("{id}/invoices")]
//         public IActionResult GetInvoices(Guid id)
//         {
//             var invoices = _db.Invoices.Where(i => i.EstimateId == id).ToList();
//             return Ok(invoices);
//         }

//         [HttpGet("{id}/expenses")]
//         public IActionResult GetExpenses(Guid id)
//         {
//             var expenses = _db.Expenses.Where(e => e.EstimateId == id).ToList();
//             return Ok(expenses);
//         }

//         [HttpGet("{id}/lines")]
//         public IActionResult GetEstimateLines(Guid id)
//         {
//             var lines = _db.EstimateLines.Where(l => l.EstimateId == id).ToList();
//             return Ok(lines);
//         }


//         [HttpPost("{id}/approve-parallel")]
//         public IActionResult ApproveParallel(Guid id, [FromBody] ApproveParallelRequest req)
//         {
//             var estimate = _workflowService.Get("Estimate", id) as Estimate;
//             if (estimate == null) return NotFound();

//             var step = _workflow.GetCurrentStep("Estimate", estimate.Status);
//             if (step?.Type != "Parallel") return BadRequest("Not a parallel step");

//             // Запись в историю
//             _db.WorkflowStepHistories.Add(new WorkflowStepHistory
//             {
//                 Id = Guid.NewGuid(),
//                 EntityId = id,
//                 EntityType = "Estimate",
//                 StepName = step.Name,
//                 SubStepName = req.SubStepName,
//                 Status = estimate.Status,
//                 UserId = UserId,
//                 UserFio = UserFio,
//                 DateTime = DateTime.UtcNow,
//                 Action = "Approve",
//                 Comment = req.Comment,
//                 Result = "Success",
//                 UserRole = req.UserRole
//             });
//             _db.SaveChanges();

//             // Проверка — все ли согласовали?
//             var allRoles = step.SubSteps.Select(s => s.Actor).ToList();
//             if (_workflow.IsAllParallelApproved(id, "Estimate", step.Name, allRoles))
//             {
//                 estimate.Status = "AccountantApproval";
//                 _db.SaveChanges();
//             }
//             return Ok();
//         }



//         // Согласование
//         [HttpPost("{id}/approve")]
//         public IActionResult Approve(Guid id, [FromBody] WorkflowActionRequest req)
//         {
//             var result = _workflowService.TryTransition(
//                 "Estimate", id, "Approved", UserId!, UserFio!, req.Comment ?? "", UserRole!, out var error);
//             if (!result) return BadRequest(error);
//             return Ok();
//         }

//         // Отказ
//         [HttpPost("{id}/reject")]
//         public IActionResult Reject(Guid id, [FromBody] WorkflowActionRequest req)
//         {
//             var result = _workflowService.TryTransition(
//                 "Estimate", id, "Rejected", UserId!, UserFio!, req.Comment ?? "", UserRole!, out var error);
//             if (!result) return BadRequest(error);
//             return Ok();
//         }

//         // Возврат на доработку (например, в Draft)
//         [HttpPost("{id}/return")]
//         public IActionResult Return(Guid id, [FromBody] WorkflowActionRequest req)
//         {
//             var result = _workflowService.TryTransition(
//                 "Estimate", id, "Draft", UserId!, UserFio!, req.Comment ?? "", UserRole!, out var error);
//             if (!result) return BadRequest(error);
//             return Ok();
//         }

//         // История маршрута
//         [HttpGet("{id}/history")]
//         public IActionResult GetHistory(Guid id)
//         {
//             var history = _workflowService.GetHistory("Estimate", id);
//             return Ok(history);
//         }

//         // Текущий маршрут (шаги и переходы)
//         [HttpGet("{id}/route")]
//         public IActionResult GetRoute(Guid id)
//         {
//             var route = _workflowService.GetWorkflowRoute("Estimate", id);
//             return Ok(route);
//         }
//     }
// }
