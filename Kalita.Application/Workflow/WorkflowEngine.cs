using System.Text.Json;
using Kalita.Application.Workflow;
using Kalita.Infrastructure.Persistence; // Для доступа к БД

namespace Kalita.Application.Workflow
{
    public class WorkflowEngine
    {
        private readonly Dictionary<string, WorkflowRouteConfig> _configs = new();
        private readonly AppDbContext _db; // Добавь DI в конструктор

        public WorkflowEngine(AppDbContext db, string configsDirPath)
        {
            _db = db;
            // Подгружаем ВСЕ json-файлы из папки configs
            foreach (var file in Directory.GetFiles(configsDirPath, "*.workflow.json"))
            {
                var json = File.ReadAllText(file);
                var options = new JsonSerializerOptions
                {
                    PropertyNameCaseInsensitive = true
                };
                options.Converters.Add(new System.Text.Json.Serialization.JsonStringEnumConverter());
                var config = JsonSerializer.Deserialize<WorkflowRouteConfig>(json, options)
                             ?? throw new Exception("Workflow config not found or invalid.");
                _configs[config.Entity] = config; // Entity должен быть в json-конфиге!
            }
        }

        public bool IsAllParallelApproved(Guid entityId, string entityType, string stepName, List<string> roles)
        {
            var approvals = _db.WorkflowStepHistories
                .Where(h => h.EntityId == entityId
                    && h.EntityType == entityType
                    && h.StepName == stepName
                    && h.Action == "Approve")
                .Select(h => h.UserRole)
                .Distinct()
                .ToList();

            // Все роли из substeps должны быть среди approvals
            return roles.All(r => approvals.Contains(r));
        }

        public WorkflowRouteConfig GetConfig(string entityType)
        {
            if (_configs.TryGetValue(entityType, out var config))
                return config;
            throw new Exception($"Workflow config for entity '{entityType}' not found.");
        }

        // Теперь все методы принимают entityType и используют соответствующий config
        public bool CanTransition(string entityType, string currentStatus, string nextStatus, object entity, string userRole)
        {
            var _config = GetConfig(entityType);

            var transition = _config.Transitions
                .FirstOrDefault(t => (t.From == currentStatus || t.From == "Any") && t.To == nextStatus);

            if (transition == null) return false;

            if (!string.IsNullOrEmpty(transition.Condition))
            {
                if (transition.Condition == "FieldsFilled")
                {
                    var step = _config.Steps.FirstOrDefault(s => s.Status == currentStatus);
                    if (step?.FieldsRequired != null)
                    {
                        foreach (var field in step.FieldsRequired)
                        {
                            var prop = entity.GetType().GetProperty(field);
                            if (prop == null || prop.GetValue(entity) == null)
                                return false;
                        }
                    }
                }
            }
            var stepNext = _config.Steps.FirstOrDefault(s => s.Status == nextStatus);
            if (stepNext != null && stepNext.Actors.Any())
            {
                if (!stepNext.Actors.Contains(userRole))
                    return false;
            }
            return true;
        }

        public WorkflowStep? GetCurrentStep(string entityType, string status)
        {
            var _config = GetConfig(entityType);
            return _config.Steps.FirstOrDefault(s => s.Status == status);
        }


    }
}