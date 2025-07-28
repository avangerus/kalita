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
            LoadWorkflows(configsDirPath); // <-- новый метод для загрузки новых workflow
        }

        private void LoadWorkflows(string configsDirPath)
        {
            foreach (var file in Directory.GetFiles(configsDirPath, "*.workflow.json"))
            {
                var json = File.ReadAllText(file);
                var options = new JsonSerializerOptions
                {
                    PropertyNameCaseInsensitive = true
                };
                var def = JsonSerializer.Deserialize<WorkflowDefinition>(json, options)
                          ?? throw new Exception("WorkflowDefinition not found or invalid in " + file);
                _workflows[def.Type] = def;
            }
        }


        public WorkflowDefinition GetWorkflow(string entityType)
        {
            if (_workflows.TryGetValue(entityType, out var definition))
                return definition;
            throw new Exception($"Workflow not found for entityType: {entityType}");
        }

        public WorkflowAction? FindAction(string entityType, string currentStatus, string actionCode)
        {
            var workflow = GetWorkflow(entityType);
            // Найти статус по currentStatus (без учета регистра)
            var status = workflow.Statuses.FirstOrDefault(s => string.Equals(s.Code, currentStatus, StringComparison.OrdinalIgnoreCase));
            if (status == null)
                return null;
            // Найти action по коду (без учета регистра)
            var action = status.Actions?.FirstOrDefault(a => string.Equals(a.Code, actionCode, StringComparison.OrdinalIgnoreCase));
            if (action != null)
                return action;
            // Проверить глобальные actions (если есть)
            action = workflow.GlobalActions?.FirstOrDefault(a =>
                string.Equals(a.Code, actionCode, StringComparison.OrdinalIgnoreCase) &&
                (a.FromStatuses == null
                    || a.FromStatuses.Any(fs => string.Equals(fs, "Any", StringComparison.OrdinalIgnoreCase))
                    || a.FromStatuses.Any(fs => string.Equals(fs, currentStatus, StringComparison.OrdinalIgnoreCase))));
            return action;
        }

        public bool CanTransition(string entityType, string currentStatus, string actionCode, string userRole, object? entityData = null)
        {
            var workflow = GetWorkflow(entityType);
            if (workflow == null)
                return false;

            // Найти статус по коду (без учета регистра)
            var status = workflow.Statuses.FirstOrDefault(s => string.Equals(s.Code, currentStatus, StringComparison.OrdinalIgnoreCase));
            WorkflowAction? action = null;
            if (status != null && status.Actions != null)
                action = status.Actions.FirstOrDefault(a => string.Equals(a.Code, actionCode, StringComparison.OrdinalIgnoreCase));

            // Если нет локального, ищем в глобальных actions
            if (action == null && workflow.GlobalActions != null)
                action = workflow.GlobalActions
                    .FirstOrDefault(a => string.Equals(a.Code, actionCode, StringComparison.OrdinalIgnoreCase) &&
                        (a.FromStatuses == null
                            || a.FromStatuses.Any(fs => string.Equals(fs, "Any", StringComparison.OrdinalIgnoreCase))
                            || a.FromStatuses.Any(fs => string.Equals(fs, currentStatus, StringComparison.OrdinalIgnoreCase))));

            if (action == null)
                return false;

            // Проверяем роль (без учета регистра)
            if (action.Roles != null && !action.Roles.Any(r => string.Equals(r, userRole, StringComparison.OrdinalIgnoreCase)))
                return false;

            // (Можно добавить сюда обработку conditions — если появится необходимость)
            // if (action.Conditions != null) { ... }

            return true;
        }


        // public WorkflowTransition? FindTransition(string entityType, string currentState, string action)
        // {
        //     var workflow = GetWorkflow(entityType);
        //     return workflow.Transitions.FirstOrDefault(t => t.From == currentState && t.Action == action);
        // }

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
        // public bool CanTransition(string entityType, string currentState, string action, Dictionary<string, object?> entityData, string userRole, string? comment = null)
        // {
        //     var transition = FindTransition(entityType, currentState, action);
        //     if (transition == null) return false;

        //     // Проверка ролей
        //     if (transition.Roles != null && !transition.Roles.Contains(userRole))
        //         return false;

        //     // Проверка условий
        //     if (transition.Conditions != null)
        //     {
        //         foreach (var cond in transition.Conditions)
        //         {
        //             if (!CheckCondition(entityData, cond))
        //                 return false;
        //         }
        //     }

        //     // Проверка комментария
        //     if (transition.CommentRequired == true && string.IsNullOrWhiteSpace(comment))
        //         return false;

        //     return true;
        // }

        // private bool CheckCondition(Dictionary<string, object?> entityData, WorkflowCondition cond)
        // {
        //     if (!entityData.TryGetValue(cond.Field, out var value) || value == null)
        //         return false;
        //     try
        //     {
        //         switch (cond.Operator)
        //         {
        //             case "<":
        //                 return Convert.ToDecimal(value) < Convert.ToDecimal(cond.Value);
        //             case ">":
        //                 return Convert.ToDecimal(value) > Convert.ToDecimal(cond.Value);
        //             case "==":
        //                 return value.ToString() == cond.Value.ToString();
        //             case "!=":
        //                 return value.ToString() != cond.Value.ToString();
        //             default:
        //                 return false;
        //         }
        //     }
        //     catch
        //     {
        //         return false;
        //     }
        // }
    }
}
