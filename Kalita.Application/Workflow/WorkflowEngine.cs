using System.Text.Json;
using Kalita.Infrastructure.Persistence; // Если тебе нужен db, оставь, если нет — убери
using System.Collections.Generic;
using Kalita.Application.Workflow;


namespace Kalita.Application.Workflow
{
    public class WorkflowEngine
    {
        private readonly Dictionary<string, WorkflowDefinition> _workflows = new();
        // private readonly AppDbContext _db; // Можно убрать, если не используешь db тут
        private readonly Dictionary<string, WorkflowRouteConfig> _configs = new();
        private readonly AppDbContext _db;

        public WorkflowEngine(AppDbContext db, string configsDirPath)
        {
            _db = db;

            // примерная реализация
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
                _configs[config.Entity] = config;
            }
        }

        public WorkflowDefinition GetWorkflow(string entityType)
        {
            if (_workflows.TryGetValue(entityType, out var definition))
                return definition;
            throw new Exception($"Workflow not found for entityType: {entityType}");
        }

        public WorkflowTransition? FindTransition(string entityType, string currentState, string action)
        {
            var workflow = GetWorkflow(entityType);
            return workflow.Transitions.FirstOrDefault(t => t.From == currentState && t.Action == action);
        }

        public WorkflowRouteConfig GetConfig(string entityType)
        {
            if (_configs.TryGetValue(entityType, out var config))
                return config;
            throw new Exception($"Workflow config for entity '{entityType}' not found.");
        }

        public WorkflowStep? GetCurrentStep(string entityType, string status)
        {
            var _config = GetConfig(entityType);
            return _config.Steps.FirstOrDefault(s => s.Status == status);
        }

        // Пример: Проверка, можно ли сделать переход
        public bool CanTransition(string entityType, string currentState, string action, Dictionary<string, object?> entityData, string userRole, string? comment = null)
        {
            var transition = FindTransition(entityType, currentState, action);
            if (transition == null) return false;

            // Проверка ролей
            if (transition.Roles != null && !transition.Roles.Contains(userRole))
                return false;

            // Проверка условий
            if (transition.Conditions != null)
            {
                foreach (var cond in transition.Conditions)
                {
                    if (!CheckCondition(entityData, cond))
                        return false;
                }
            }

            // Проверка комментария
            if (transition.CommentRequired == true && string.IsNullOrWhiteSpace(comment))
                return false;

            return true;
        }

        private bool CheckCondition(Dictionary<string, object?> entityData, WorkflowCondition cond)
        {
            if (!entityData.TryGetValue(cond.Field, out var value) || value == null)
                return false;
            try
            {
                switch (cond.Operator)
                {
                    case "<":
                        return Convert.ToDecimal(value) < Convert.ToDecimal(cond.Value);
                    case ">":
                        return Convert.ToDecimal(value) > Convert.ToDecimal(cond.Value);
                    case "==":
                        return value.ToString() == cond.Value.ToString();
                    case "!=":
                        return value.ToString() != cond.Value.ToString();
                    default:
                        return false;
                }
            }
            catch
            {
                return false;
            }
        }
    }
}
