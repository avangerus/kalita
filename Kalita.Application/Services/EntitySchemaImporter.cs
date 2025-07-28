using System.Text.Json;
using System.Text.Json.Serialization;
using Kalita.Domain.Entities;
using Kalita.Infrastructure.Persistence;

public class EntitySchemaImporter
{
    private readonly AppDbContext _db;
    public EntitySchemaImporter(AppDbContext db) => _db = db;

    // DTO для работы с твоим JSON
    public class ImportEntityType
    {
        public string Name { get; set; }
        public string DisplayName { get; set; }
        public string? Description { get; set; }
        public List<ImportEntityField> Fields { get; set; }
    }

    public class ImportEntityField
    {
        public string Name { get; set; }
        public string DisplayName { get; set; }
        public string Type { get; set; }
        public bool? Required { get; set; }
        public string? Ref { get; set; }
        public bool? Multi { get; set; }
        public List<string>? Values { get; set; }
        public string? Formula { get; set; }
        public List<string>? EnumOptions { get; set; }
    }

    public void ImportFromFile(string path)
    {
        var json = File.ReadAllText(path);

        var entities = JsonSerializer.Deserialize<List<ImportEntityType>>(json, new JsonSerializerOptions
        {
            PropertyNameCaseInsensitive = true
        });

        if (entities == null)
            throw new Exception("Не удалось десериализовать файл схемы.");




        foreach (var dto in entities)
        {
            // Ищем существующий тип по коду или создаём новый
            var code = dto.Name;
            var type = _db.EntityTypes.FirstOrDefault(t => t.Code == code);
            if (type == null)
            {
                type = new EntityType
                {
                    Id = Guid.NewGuid(),
                    Code = code,
                    DisplayName = dto.DisplayName,
                    Description = dto.Description ?? ""
                };
                _db.EntityTypes.Add(type);
                _db.SaveChanges();
            }
            else
            {
                type.DisplayName = dto.DisplayName;
                type.Description = dto.Description ?? "";
                _db.SaveChanges();
            }

            // Чистим старые поля
            var oldFields = _db.EntityFields.Where(f => f.EntityTypeId == type.Id).ToList();
            _db.EntityFields.RemoveRange(oldFields);
            _db.SaveChanges();

            // Добавляем новые поля
            foreach (var fieldDto in dto.Fields)
            {
                var field = new EntityField
                {
                    Id = Guid.NewGuid(),
                    EntityTypeId = type.Id,
                    Code = fieldDto.Name,
                    DisplayName = fieldDto.DisplayName,
                    FieldType = fieldDto.Type,
                    IsRequired = fieldDto.Required ?? false,
                    IsMultiValue = fieldDto.Multi ?? false,
                    LookupTypeCode = fieldDto.Ref ?? "",
                        EnumOptions = fieldDto.Values != null
        ? JsonSerializer.Serialize(fieldDto.Values)
        : ""
                        ,
                    Description = "",      // Можно расширить, если появится
                    DefaultValue = "",     // Можно расширить, если появится
                };
                _db.EntityFields.Add(field);
            }
            _db.SaveChanges();

        }
    }
}
