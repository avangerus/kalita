// Kalita.Application/Services/WorkflowEntityService.cs

using Kalita.Domain.Entities;
using Kalita.Infrastructure.Persistence;
using Kalita.Application.Workflow;
using System.Text.Json;

namespace Kalita.Application.Services
{
    /// <summary>
    /// Универсальный сервис для работы с маршрутами и историей согласования любой сущности, поддерживающей Workflow
    /// </summary>
    public class WorkflowEntityService
    {
        private readonly AppDbContext _db;
        private readonly WorkflowEngine _workflow;

        public WorkflowEntityService(AppDbContext db, WorkflowEngine workflow)
        {
            _db = db;
            _workflow = workflow;
        }

        /// <summary>
        /// Получить одну сущность (EntityItem) по типу и id
        /// </summary>
        public EntityItem? Get(string entityType, Guid id)
        {
            return _db.EntityItems.FirstOrDefault(e => e.Id == id && e.TypeCode == entityType);
        }

        /// <summary>
        /// Получить все сущности по типу
        /// </summary>
        public IEnumerable<EntityItem> GetAll(string entityType)
        {
            return _db.EntityItems.Where(e => e.TypeCode == entityType).ToList();
        }

        /// <summary>
        /// Переход по маршруту для EntityItem (универсально для всех типов)
        /// </summary>
        public bool TryTransition(
            string entityType,
            Guid entityId,
            string nextStatus,
            string userId,
            string userFio,
            string comment,
            string userRole,
            out string error)
        {
            var item = Get(entityType, entityId);
            if (item == null)
            {
                error = $"{entityType} not found.";
                return false;
            }

            var currentStatus = item.Status ?? "";
            var data = JsonSerializer.Deserialize<Dictionary<string, object?>>(item.DataJson ?? "{}");

            if (!_workflow.CanTransition(entityType, currentStatus, nextStatus, data, userRole, comment))
            {
                error = "Transition not allowed.";
                return false;
            }

            // Сохраняем историю перехода
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

            // Обновляем статус сущности
            item.Status = nextStatus;
            _db.SaveChanges();

            error = "";
            return true;
        }

        /// <summary>
        /// Получить "маршрут" (workflow) по сущности
        /// </summary>
        public object GetWorkflowRoute(string entityType, Guid entityId)
        {
            var entity = Get(entityType, entityId);
            if (entity == null)
                throw new Exception($"{entityType} not found.");

            var currentStatus = entity.Status ?? "";
            var config = _workflow.GetConfig(entityType);

            return new
            {
                CurrentStatus = currentStatus,
                Steps = config.Steps,
                Transitions = config.Transitions
            };
        }

        // ----- Блок работы со справочниками -----

        public (bool Success, DictionaryType? Type, string Error) CreateDictionaryType(string code, string name, string? description)
        {
            var type = new DictionaryType
            {
                Id = Guid.NewGuid(),
                Code = code,
                Name = name,
                Description = description
            };
            _db.DictionaryTypes.Add(type);
            _db.SaveChanges();
            return (true, type, "");
        }

        public List<DictionaryType> GetDictionaryTypes()
            => _db.DictionaryTypes.ToList();

        public (bool Success, DictionaryItem? Item, string Error) CreateDictionaryItem(Guid typeId, string value, string code, string? extraJson)
        {
            var item = new DictionaryItem
            {
                Id = Guid.NewGuid(),
                TypeId = typeId,
                Value = value,
                Code = code,
                ExtraJson = extraJson
            };
            _db.DictionaryItems.Add(item);
            _db.SaveChanges();
            return (true, item, "");
        }

        public List<DictionaryItem> GetDictionaryItems(Guid typeId)
            => _db.DictionaryItems.Where(x => x.TypeId == typeId && x.IsActive).ToList();

        public List<DictionaryItem> GetDictionaryItemsByTypeCode(string code)
        {
            var type = _db.DictionaryTypes.FirstOrDefault(t => t.Code == code);
            if (type == null) return new List<DictionaryItem>();
            return _db.DictionaryItems.Where(x => x.TypeId == type.Id && x.IsActive).ToList();
        }

        /// <summary>
        /// Получить историю маршрута по сущности
        /// </summary>
        public List<WorkflowStepHistory> GetHistory(string entityType, Guid entityId)
        {
            return _db.WorkflowStepHistories
                .Where(h => h.EntityId == entityId && h.EntityType == entityType)
                .OrderBy(h => h.DateTime)
                .ToList();
        }
    }
}
