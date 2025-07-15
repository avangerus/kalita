using Microsoft.AspNetCore.Mvc;

[ApiController]
[Route("api/entitytypes")]
public class EntityTypesController : ControllerBase
{
    private readonly EntityTypeService _service;

    public EntityTypesController(EntityTypeService service)
    {
        _service = service;
    }

    [HttpGet]
    public IActionResult GetAll()
    {
        var types = _service.GetAllTypes()
            .Select(t => new {
                t.Code,
                t.DisplayName,
                t.Description
            })
            .ToList();

        return Ok(types);
    }

    [HttpGet("{code}")]
    public IActionResult GetOne(string code)
    {
        var type = _service.GetTypeByCode(code);
        if (type == null) return NotFound();

        var fields = _service.GetFieldsByTypeCode(code)
            .Select(f => new {
                f.Code,
                f.DisplayName,
                f.FieldType,
                f.IsRequired,
                f.LookupTypeCode,
                f.IsMultiValue
            })
            .ToList();

        var result = new {
            type.Code,
            type.DisplayName,
            type.Description,
            Fields = fields
        };

        return Ok(result);
    }
}
