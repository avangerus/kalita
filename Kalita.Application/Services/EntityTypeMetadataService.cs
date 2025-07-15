// using System.Text.Json;

// public class EntityTypeMetadataService
// {
//     public class EntityTypeDefinition
//     {
//         public string Code { get; set; } = "";
//         public string Name { get; set; } = "";
//         public string? Description { get; set; }
//         public List<FieldDefinition> Fields { get; set; } = new();
//     }
//     public class FieldDefinition
//     {
//         public string Code { get; set; } = "";
//         public string Type { get; set; } = "string";
//         public bool Required { get; set; }
//     }

//     private List<EntityTypeDefinition> _entities = new();

//     public EntityTypeMetadataService(string jsonPath)
//     {
//         var json = File.ReadAllText(jsonPath);
//         _entities = JsonSerializer.Deserialize<List<EntityTypeDefinition>>(json)
//                    ?? new List<EntityTypeDefinition>();
//     }

//     public List<EntityTypeDefinition> GetAll() => _entities;
//     public EntityTypeDefinition? GetByCode(string code) => _entities.FirstOrDefault(e => e.Code == code);
// }
