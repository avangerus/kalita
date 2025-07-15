// Kalita.Application/Services/ValidationService.cs

using Kalita.Domain.Entities;
using Kalita.Application.DTO; // Используй правильный namespace для EntityTypeDto и EntityFieldDto
using System.Text.Json;
using System.Text.RegularExpressions;

namespace Kalita.Application.Services
{
    /// <summary>
    /// Валидатор динамических сущностей по их метаданным.
    /// </summary>
    public class ValidationService
    {
        /// <summary>
        /// Валидирует объект EntityItem на основе метаданных (EntityTypeDto).
        /// </summary>
        /// <param name="item">Сохраняемая/изменяемая сущность</param>
        /// <param name="meta">Мета-схема типа</param>
        /// <returns>(успех, сообщение об ошибке)</returns>
        public (bool Valid, string Error) Validate(EntityItem item, EntityTypeDto meta)
        {
            if (item == null)
                return (false, "EntityItem is null");
            if (meta == null)
                return (false, "EntityType metadata not found");

            Dictionary<string, object?>? data = null;
            try
            {
                data = JsonSerializer.Deserialize<Dictionary<string, object?>>(item.DataJson ?? "{}");
            }
            catch
            {
                return (false, "Invalid JSON in entity data");
            }
            if (data == null)
                return (false, "Empty or invalid entity data");

            foreach (var field in meta.Fields)
            {
                // Проверка обязательных
                if (field.Required)
                {
                    if (!data.ContainsKey(field.Code) || data[field.Code] == null || string.IsNullOrWhiteSpace(data[field.Code]?.ToString()))
                        return (false, $"Поле \"{field.Name ?? field.Code}\" обязательно для заполнения");
                }

                // Если не заполнено — дальше не валидируем
                if (!data.ContainsKey(field.Code) || data[field.Code] == null)
                    continue;

                var value = data[field.Code];

                // Типизация через JsonElement (если значение пришло из JSON)
                JsonElement je = default;
                if (value is JsonElement jeRaw)
                    je = jeRaw;
                else if (value is string s)
                    je = JsonSerializer.Deserialize<JsonElement>($"\"{s}\"");

                // Валидация по FieldType
                switch (field.FieldType)
                {
                    case "number":
                        if (value is JsonElement jeNum && jeNum.ValueKind == JsonValueKind.Number)
                            break;
                        if (decimal.TryParse(value?.ToString(), out _))
                            break;
                        return (false, $"Поле \"{field.Name ?? field.Code}\" должно быть числом");
                    case "bool":
                        if (value is JsonElement jeBool)
                        {
                            if (jeBool.ValueKind == JsonValueKind.True || jeBool.ValueKind == JsonValueKind.False)
                                break;
                        }
                        if (bool.TryParse(value?.ToString(), out _))
                            break;
                        return (false, $"Поле \"{field.Name ?? field.Code}\" должно быть булевым значением");
                    case "string":
                        if (value is JsonElement jeStr && jeStr.ValueKind == JsonValueKind.String)
                            break;
                        if (value is string)
                            break;
                        return (false, $"Поле \"{field.Name ?? field.Code}\" должно быть строкой");
                    case "date":
                        if (value is JsonElement jeDate && jeDate.ValueKind == JsonValueKind.String && DateTime.TryParse(jeDate.GetString(), out _))
                            break;
                        if (DateTime.TryParse(value?.ToString(), out _))
                            break;
                        return (false, $"Поле \"{field.Name ?? field.Code}\" должно быть датой");
                    case "ref":
                        // Для ref обычно ожидаем строку GUID или просто не пустое значение
                        if (value is JsonElement jeRef && jeRef.ValueKind == JsonValueKind.String && Guid.TryParse(jeRef.GetString(), out _))
                            break;
                        if (Guid.TryParse(value?.ToString(), out _))
                            break;
                        return (false, $"Поле \"{field.Name ?? field.Code}\" должно быть ссылкой (GUID)");
                        // Можно добавить custom case для enum/справочник
                }
                if (field.FieldType == "string")
                {
                    var str = value?.ToString() ?? "";
                    if (field.MinLength.HasValue && str.Length < field.MinLength.Value)
                        return (false, $"Поле \"{field.Name}\" должно содержать минимум {field.MinLength.Value} символов");
                    if (field.MaxLength.HasValue && str.Length > field.MaxLength.Value)
                        return (false, $"Поле \"{field.Name}\" должно содержать не более {field.MaxLength.Value} символов");
                    if (!string.IsNullOrEmpty(field.Pattern))
                    {
                        if (!Regex.IsMatch(str, field.Pattern))
                            return (false, $"Поле \"{field.Name}\" не соответствует формату");
                    }
                }
                if (field.FieldType == "number" && decimal.TryParse(value?.ToString(), out var number))
                {
                    if (field.MinValue.HasValue && number < field.MinValue.Value)
                        return (false, $"Поле \"{field.Name}\" не может быть меньше {field.MinValue.Value}");
                    if (field.MaxValue.HasValue && number > field.MaxValue.Value)
                        return (false, $"Поле \"{field.Name}\" не может быть больше {field.MaxValue.Value}");
                }
                if (field.AllowedValues != null && field.AllowedValues.Any() && !field.AllowedValues.Contains(value?.ToString()))
                {
                    return (false, $"Поле \"{field.Name}\" содержит недопустимое значение");
                }
            }




            // Пример: Бизнес-валидация для EstimateLine — обязательно должен быть GroupId
            // if (meta.Code == "EstimateLine" && (!data.ContainsKey("GroupId") || data["GroupId"] == null))
            //     return (false, "У EstimateLine обязательно должен быть GroupId");

            // Можно добавить формулы, проверки на уникальность, пересечения и прочее здесь

            return (true, "");
        }
    }
}
