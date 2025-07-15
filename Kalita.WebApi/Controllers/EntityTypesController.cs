using Microsoft.AspNetCore.Mvc;

[ApiController]
[Route("api/entitytypes")]
public class EntityTypesController : ControllerBase
{
    private readonly EntityMetadataService _service;

    public EntityTypesController(EntityMetadataService service)
    {
        _service = service;
    }

    [HttpGet]
    public IActionResult GetAll() => Ok(_service.GetAllTypes());

    [HttpGet("{code}")]
    public IActionResult GetOne(string code)
    {
        var meta = _service.GetTypeByCode(code);
        if (meta == null) return NotFound();
        return Ok(meta);
    }
}
