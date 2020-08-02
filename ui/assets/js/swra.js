$(function () {
    // Select2.js multiple select boxes
    if (jQuery.fn.select2)
        $('select[multiple]').select2();

    // Cleave input formatting
    if (typeof(Cleave) != 'undefined') {
        new Cleave('.mac-input', {
            delimiter: ':',
            blocks: new Array(6).fill(2),
            uppercase: true,
        });
    }

    // Submit a form via XHR when a value of an input changes
    $('.submit-on-change').change(function(ev) {
        var form = $(ev.target.form);
        $.post(form.attr('action'), form.serialize(), function(data) {
            toastr[data.Type](data.Message, data.Title);
        }, 'json')
    });

    // Confirm deletion before submitting form
    $('.confirm-delete').submit(function(ev) {
        var type = $(ev.target).data('type');
        var name = $(ev.target).data('name');
        if (!confirm("Do you really want to delete the '" + name + "' " + type + "?"))
            ev.preventDefault();
    });
});
