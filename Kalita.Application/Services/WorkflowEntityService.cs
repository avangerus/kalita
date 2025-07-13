// Kalita.Application/Services/WorkflowEntityService.cs

using Kalita.Domain.Entities;
using Kalita.Infrastructure.Persistence;

using Kalita.Application.Workflow;

namespace Kalita.Application.Services
{
    public class WorkflowEntityService
    {
        private readonly AppDbContext _db;
        private readonly WorkflowEngine _workflow;

        public WorkflowEntityService(AppDbContext db, WorkflowEngine workflow)
        {
            _db = db;
            _workflow = workflow;
        }

        // Generic CRUD
        public object? Get(string entityType, Guid id)
        {
            return entityType switch
            {
                "Estimate" => _db.Estimates.FirstOrDefault(e => e.Id == id),
                "Expense" => _db.Expenses.FirstOrDefault(e => e.Id == id),
                "Invoice" => _db.Invoices.FirstOrDefault(e => e.Id == id),
                _ => null
            };
        }

        public IEnumerable<object> GetAll(string entityType)
        {
            return entityType switch
            {
                "Estimate" => _db.Estimates.ToList(),
                "Expense" => _db.Expenses.ToList(),
                "Invoice" => _db.Invoices.ToList(),
                _ => new List<object>()
            };
        }

        // Универсальный переход по маршруту
        public bool TryTransition(string entityType, Guid entityId, string nextStatus, Guid userId, string userFio, string comment, string userRole, out string error)
        {
            var entity = Get(entityType, entityId);
            if (entity == null)
            {
                error = $"{entityType} not found.";
                return false;
            }

            // Получаем текущее значение статуса
            var statusProp = entity.GetType().GetProperty("Status");
            if (statusProp == null)
            {
                error = "Entity does not have 'Status' property.";
                return false;
            }
            var currentStatus = statusProp.GetValue(entity)?.ToString() ?? "";

            if (!_workflow.CanTransition(entityType, currentStatus, nextStatus, entity, userRole))
            {
                error = "Transition not allowed.";
                return false;
            }

            // Сохраняем историю
            var history = new WorkflowStepHistory
            {
                Id = Guid.NewGuid(),
                EntityId = entityId,
                EntityType = entityType,
                StepName = _workflow.GetCurrentStep(entityType, currentStatus)?.Name ?? currentStatus,
                Status = nextStatus,
                UserId = userId,
                UserFio = userFio,
                DateTime = DateTime.UtcNow,
                Action = "Transition",
                Comment = comment,
                Result = "Success"
            };
            _db.WorkflowStepHistories.Add(history);

            // Обновляем статус
            statusProp.SetValue(entity, nextStatus);

            _db.SaveChanges();
            error = "";
            return true;
        }

        public List<WorkflowStepHistory> GetHistory(string entityType, Guid entityId)
        {
            return _db.WorkflowStepHistories
                .Where(h => h.EntityId == entityId && h.EntityType == entityType)
                .OrderBy(h => h.DateTime)
                .ToList();
        }
    }
}